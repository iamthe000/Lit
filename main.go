package main

import (
	"crypto/sha256"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

// --- Data models ---
type Config struct {
	MaxVersions int    `json:"max_versions"`
	Language    string `json:"language"`
}

type Commit struct {
	ID        string    `json:"id"`
	ParentID  string    `json:"parent_id"`
	Full      bool      `json:"full"`
	Deleted   []string  `json:"deleted,omitempty"`
	Message   string    `json:"message"`
	Timestamp time.Time `json:"timestamp"`
}

const (
	LitDir      = ".lit"
	ConfigFile  = ".lit/config.json"
	HistoryFile = ".lit/history.json"
	ObjectsDir  = ".lit/objects"
	ProjectsDB  = ".lit-projects.json"
)

const (
	LanguageEN = "en"
	LanguageJP = "jp"
)

type ProjectEntry struct {
	Name      string    `json:"name"`
	Path      string    `json:"path"`
	CreatedAt time.Time `json:"created_at"`
}

// --- CLI routing ---

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	command := os.Args[1]

	switch command {
	case "init":
		initCmd := flag.NewFlagSet("init", flag.ExitOnError)
		maxVersions := initCmd.Int("max", 10, "maximum number of versions to keep")
		initCmd.Parse(os.Args[2:])
		if initCmd.NArg() > 0 {
			if n, err := fmt.Sscanf(initCmd.Arg(0), "%d", maxVersions); n == 1 && err == nil {
			}
		}
		runInit(*maxVersions)

	case "commit":
		commitCmd := flag.NewFlagSet("commit", flag.ExitOnError)
		message := commitCmd.String("m", "Auto commit", "commit message")
		commitCmd.Parse(os.Args[2:])
		runCommit(*message, commitCmd.Args())

	case "log":
		runLog()

	case "ls":
		runListProjects()

	case "en":
		runSetLanguage(LanguageEN)

	case "jp":
		runSetLanguage(LanguageJP)

	case "diff":
		if len(os.Args) < 3 {
			fmt.Println("Error: specify a commit ID to compare. Example: lit diff <commit_id>")
			os.Exit(1)
		}
		if len(os.Args) >= 4 {
			if isCommitID(os.Args[3]) {
				if len(os.Args) >= 5 {
					runDiffCommitsFile(os.Args[2], os.Args[3], os.Args[4])
				} else {
					runDiffCommits(os.Args[2], os.Args[3])
				}
			} else {
				runDiffFile(os.Args[2], os.Args[3])
			}
		} else {
			runDiff(os.Args[2])
		}

	case "revert":
		if len(os.Args) < 3 {
			fmt.Println("Error: specify a commit ID to restore. Example: lit revert <commit_id>")
			os.Exit(1)
		}
		runRevert(os.Args[2])

	default:
		fmt.Printf("Error: unknown command: %s\n", command)
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	const green = "\x1b[32m"
	const reset = "\x1b[0m"

	if isJapanese() {
		fmt.Print(`Lit by-mininium(iamthe000)

` + green + `
        __    _ __ 
       / /   (_) /_
      / /   / / __/
     / /___/ / /_  
    /_____/_/\__/  ` + reset + `
               

使用方法:
  lit init [-max <n>]      現在のディレクトリをプロジェクトとして初期化
  lit commit [-m "<msg>"] [files...]  指定ファイルだけ、または変更を保存
  lit log                  コミット履歴を表示
  lit ls                   初期化済みプロジェクト一覧を表示
  lit diff <id>            指定コミットとの差分を表示
  lit revert <id>          指定コミットの状態に復元
  lit en                   表示言語を英語に切り替え
  lit jp                   表示言語を日本語に切り替え`)
		return
	}

	fmt.Print(`Lit by-mininium(iamthe000)

` + green + `
        __    _ __ 
       / /   (_) /_
      / /   / / __/
     / /___/ / /_  
    /_____/_/\__/  ` + reset + `
               

Usage:
  lit init [-max <n>]      Initialize the current directory as a project
  lit commit [-m "<msg>"] [files...]  Save only specified files or all changes
  lit log                  Show commit history
  lit ls                   List initialized projects
  lit diff <id>            Show diff against a commit
  lit revert <id>          Restore a project to a commit
  lit en                   Switch output language to English
  lit jp                   Switch output language to Japanese`)
}

// --- コマンド実装 ---

func runInit(maxVersions int) {
	if _, err := os.Stat(LitDir); !os.IsNotExist(err) {
		printLocalized("Warning: this directory is already initialized as a Lit repository.", "このディレクトリはすでに Lit リポジトリとして初期化されています。")
		return
	}

	os.MkdirAll(ObjectsDir, 0o755)

	config := Config{MaxVersions: maxVersions, Language: LanguageEN}
	saveJSON(ConfigFile, config)
	saveJSON(HistoryFile, []Commit{})
	registerProject()

	printLocalized(
		fmt.Sprintf("Lit repository initialized. Max versions: %d\n", maxVersions),
		fmt.Sprintf("Lit リポジトリを初期化しました。最大保存世代数: %d\n", maxVersions),
	)
}

func runCommit(message string, targets []string) {
	ensureInitialized()

	config := Config{}
	loadJSON(ConfigFile, &config)

	var history []Commit
	loadJSON(HistoryFile, &history)

	commitID := fmt.Sprintf("%d", time.Now().UnixNano())[:13] // 簡易的な一意のID（タイムスタンプベース）

	parentID := ""
	if len(history) > 0 {
		parentID = history[0].ID
	}

	snapshotPath := filepath.Join(ObjectsDir, commitID)
	if err := os.MkdirAll(snapshotPath, 0o755); err != nil {
		printLocalized(fmt.Sprintf("Commit failed: %v\n", err), fmt.Sprintf("コミットに失敗しました: %v\n", err))
		os.Exit(1)
	}

	isFull := len(history) == 0 && len(targets) == 0
	changedFiles, deletedFiles, err := buildCommitPayload(parentID, isFull, targets)
	if err != nil {
		if strings.HasPrefix(err.Error(), "target file not found: ") {
			target := strings.TrimPrefix(err.Error(), "target file not found: ")
			printLocalized(
				fmt.Sprintf("Error: target '%s' was not found.\n", target),
				fmt.Sprintf("エラー: 指定された対象 '%s' が見つかりません。\n", target),
			)
		} else {
			printLocalized(fmt.Sprintf("Commit failed: %v\n", err), fmt.Sprintf("コミットに失敗しました: %v\n", err))
		}
		os.Exit(1)
	}
	for _, path := range changedFiles {
		if err := copyFile(path, filepath.Join(snapshotPath, path)); err != nil {
			printLocalized(fmt.Sprintf("Commit failed: %v\n", err), fmt.Sprintf("コミットに失敗しました: %v\n", err))
			os.Exit(1)
		}
	}
	meta := Commit{
		ID:        commitID,
		ParentID:  parentID,
		Full:      isFull,
		Deleted:   deletedFiles,
		Message:   message,
		Timestamp: time.Now(),
	}
	saveJSON(filepath.Join(snapshotPath, "meta.json"), meta)

	// 履歴の追加
	newCommit := Commit{
		ID:        commitID,
		ParentID:  parentID,
		Full:      isFull,
		Deleted:   deletedFiles,
		Message:   message,
		Timestamp: time.Now(),
	}
	history = append([]Commit{newCommit}, history...) // 先頭に追加

	// 世代管理 (MaxVersionsを超えたら古いものを削除)
	if len(history) > config.MaxVersions {
		for i := config.MaxVersions; i < len(history); i++ {
			oldCommitID := history[i].ID
			if i == config.MaxVersions && len(history) > config.MaxVersions {
				promoteCommitToFull(history[i-1].ID)
				history[i-1].Full = true
				history[i-1].ParentID = ""
				history[i-1].Deleted = nil
			}
			os.RemoveAll(filepath.Join(ObjectsDir, oldCommitID))
			printLocalized(fmt.Sprintf("Removed old version: %s\n", oldCommitID), fmt.Sprintf("古いバージョンを削除しました: %s\n", oldCommitID))
		}
		history = history[:config.MaxVersions]
	}

	saveJSON(HistoryFile, history)
	printLocalized(
		fmt.Sprintf("Commit completed: [%s] %s\n", commitID, message),
		fmt.Sprintf("コミット完了: [%s] %s\n", commitID, message),
	)
}

func runLog() {
	ensureInitialized()

	var history []Commit
	loadJSON(HistoryFile, &history)

	if len(history) == 0 {
		printLocalized("No commit history.\n", "コミット履歴はありません。\n")
		return
	}

	for _, c := range history {
		fmt.Printf("commit %s\n", c.ID)
		fmt.Printf("Date:  %s\n", c.Timestamp.Format("2006-01-02 15:04:05"))
		fmt.Printf("\n    %s\n\n", c.Message)
	}
}

func runListProjects() {
	projects := loadProjects()
	if len(projects) == 0 {
		printLocalized("No initialized projects.\n", "初期化済みプロジェクトはありません。\n")
		return
	}

	for _, p := range projects {
		fmt.Printf("%s\n", p.Path)
		if isJapanese() {
			fmt.Printf("  名前: %s\n", p.Name)
			fmt.Printf("  作成日時: %s\n\n", p.CreatedAt.Format("2006-01-02 15:04:05"))
		} else {
			fmt.Printf("  name: %s\n", p.Name)
			fmt.Printf("  created: %s\n\n", p.CreatedAt.Format("2006-01-02 15:04:05"))
		}
	}
}

func runDiff(commitID string) {
	ensureInitialized()
	targetPath, err := materializeCommit(commitID)
	if err != nil {
		printLocalized(
			fmt.Sprintf("Error: commit ID '%s' not found.\n", commitID),
			fmt.Sprintf("エラー: コミットID '%s' が見つかりません。\n", commitID),
		)
		os.Exit(1)
	}
	defer os.RemoveAll(targetPath)

	if _, err := os.Stat(targetPath); os.IsNotExist(err) {
		printLocalized(
			fmt.Sprintf("Error: commit ID '%s' not found.\n", commitID),
			fmt.Sprintf("エラー: コミットID '%s' が見つかりません。\n", commitID),
		)
		os.Exit(1)
	}

	printLocalized(fmt.Sprintf("Diff against commit [%s]:\n", commitID), fmt.Sprintf("コミット [%s] との差分:\n", commitID))
	hasDiff := false

	_ = filepath.Walk(".", func(path string, info os.FileInfo, err error) error {
		if shouldIgnore(path, []string{LitDir, ".git", ".DS_Store"}) || info.IsDir() {
			return nil
		}

		targetFile := filepath.Join(targetPath, path)
		if _, err := os.Stat(targetFile); os.IsNotExist(err) {
			printLocalized(fmt.Sprintf("  [+] Added: %s\n", path), fmt.Sprintf("  [+] 追加: %s\n", path))
			hasDiff = true
		} else {
			if hashFile(path) != hashFile(targetFile) {
				printLocalized(fmt.Sprintf("  [~] Modified: %s\n", path), fmt.Sprintf("  [~] 変更: %s\n", path))
				hasDiff = true
			}
		}
		return nil
	})

	_ = filepath.Walk(targetPath, func(path string, info os.FileInfo, err error) error {
		if info.IsDir() {
			return nil
		}
		relPath, _ := filepath.Rel(targetPath, path)
		if _, err := os.Stat(relPath); os.IsNotExist(err) {
			printLocalized(fmt.Sprintf("  [-] Deleted: %s\n", relPath), fmt.Sprintf("  [-] 削除: %s\n", relPath))
			hasDiff = true
		}
		return nil
	})

	if !hasDiff {
		printLocalized("No differences.\n", "差分はありません。\n")
	}
}

func runDiffFile(commitID, fileName string) {
	ensureInitialized()
	targetPath, err := materializeCommit(commitID)
	if err != nil {
		printLocalized(
			fmt.Sprintf("Error: commit ID '%s' not found.\n", commitID),
			fmt.Sprintf("エラー: コミットID '%s' が見つかりません。\n", commitID),
		)
		os.Exit(1)
	}
	defer os.RemoveAll(targetPath)

	currentPath := fileName
	targetFile := filepath.Join(targetPath, fileName)

	if _, err := os.Stat(targetFile); os.IsNotExist(err) {
		printLocalized(
			fmt.Sprintf("Error: file '%s' was not found in commit [%s].\n", fileName, commitID),
			fmt.Sprintf("エラー: ファイル '%s' はコミット [%s] に存在しません。\n", fileName, commitID),
		)
		os.Exit(1)
	}
	if _, err := os.Stat(currentPath); os.IsNotExist(err) {
		printLocalized(
			fmt.Sprintf("Error: file '%s' does not exist in the current directory.\n", fileName),
			fmt.Sprintf("エラー: 現在のディレクトリにファイル '%s' がありません。\n", fileName),
		)
		os.Exit(1)
	}

	currentLines, err := readLines(currentPath)
	if err != nil {
		printLocalized(fmt.Sprintf("Error: %v\n", err), fmt.Sprintf("エラー: %v\n", err))
		os.Exit(1)
	}
	targetLines, err := readLines(targetFile)
	if err != nil {
		printLocalized(fmt.Sprintf("Error: %v\n", err), fmt.Sprintf("エラー: %v\n", err))
		os.Exit(1)
	}

	printLocalized(
		fmt.Sprintf("Line diff for %s against commit [%s]:\n", fileName, commitID),
		fmt.Sprintf("ファイル %s のコミット [%s] との差分:\n", fileName, commitID),
	)
	printLineDiff(currentLines, targetLines)
}

func runDiffCommits(fromCommitID, toCommitID string) {
	ensureInitialized()
	fromPath, err := materializeCommit(fromCommitID)
	if err != nil {
		printLocalized(
			fmt.Sprintf("Error: commit ID '%s' not found.\n", fromCommitID),
			fmt.Sprintf("エラー: コミットID '%s' が見つかりません。\n", fromCommitID),
		)
		os.Exit(1)
	}
	defer os.RemoveAll(fromPath)

	toPath, err := materializeCommit(toCommitID)
	if err != nil {
		printLocalized(
			fmt.Sprintf("Error: commit ID '%s' not found.\n", toCommitID),
			fmt.Sprintf("エラー: コミットID '%s' が見つかりません。\n", toCommitID),
		)
		os.Exit(1)
	}
	defer os.RemoveAll(toPath)

	printLocalized(
		fmt.Sprintf("Diff between commits [%s] and [%s]:\n", fromCommitID, toCommitID),
		fmt.Sprintf("コミット [%s] と [%s] の差分:\n", fromCommitID, toCommitID),
	)

	fromFiles := map[string]string{}
	toFiles := map[string]string{}
	_ = collectCurrentFiles(fromPath, fromFiles)
	_ = collectCurrentFiles(toPath, toFiles)

	seen := map[string]bool{}
	changed := false

	for path, fromHash := range fromFiles {
		seen[path] = true
		toHash, ok := toFiles[path]
		if !ok {
			printLocalized(fmt.Sprintf("  [-] Deleted: %s\n", path), fmt.Sprintf("  [-] 削除: %s\n", path))
			changed = true
			continue
		}
		if fromHash != toHash {
			printLocalized(fmt.Sprintf("  [~] Modified: %s\n", path), fmt.Sprintf("  [~] 変更: %s\n", path))
			changed = true
		}
	}

	for path := range toFiles {
		if seen[path] {
			continue
		}
		printLocalized(fmt.Sprintf("  [+] Added: %s\n", path), fmt.Sprintf("  [+] 追加: %s\n", path))
		changed = true
	}

	if !changed {
		printLocalized("No differences.\n", "差分はありません。\n")
	}
}

func runDiffCommitsFile(fromCommitID, toCommitID, fileName string) {
	ensureInitialized()
	fromPath, err := materializeCommit(fromCommitID)
	if err != nil {
		printLocalized(
			fmt.Sprintf("Error: commit ID '%s' not found.\n", fromCommitID),
			fmt.Sprintf("エラー: コミットID '%s' が見つかりません。\n", fromCommitID),
		)
		os.Exit(1)
	}
	defer os.RemoveAll(fromPath)

	toPath, err := materializeCommit(toCommitID)
	if err != nil {
		printLocalized(
			fmt.Sprintf("Error: commit ID '%s' not found.\n", toCommitID),
			fmt.Sprintf("エラー: コミットID '%s' が見つかりません。\n", toCommitID),
		)
		os.Exit(1)
	}
	defer os.RemoveAll(toPath)

	fromFile := filepath.Join(fromPath, fileName)
	toFile := filepath.Join(toPath, fileName)

	if _, err := os.Stat(fromFile); os.IsNotExist(err) {
		printLocalized(
			fmt.Sprintf("Error: file '%s' was not found in commit [%s].\n", fileName, fromCommitID),
			fmt.Sprintf("エラー: ファイル '%s' はコミット [%s] に存在しません。\n", fileName, fromCommitID),
		)
		os.Exit(1)
	}
	if _, err := os.Stat(toFile); os.IsNotExist(err) {
		printLocalized(
			fmt.Sprintf("Error: file '%s' was not found in commit [%s].\n", fileName, toCommitID),
			fmt.Sprintf("エラー: ファイル '%s' はコミット [%s] に存在しません。\n", fileName, toCommitID),
		)
		os.Exit(1)
	}

	fromLines, err := readLines(fromFile)
	if err != nil {
		printLocalized(fmt.Sprintf("Error: %v\n", err), fmt.Sprintf("エラー: %v\n", err))
		os.Exit(1)
	}
	toLines, err := readLines(toFile)
	if err != nil {
		printLocalized(fmt.Sprintf("Error: %v\n", err), fmt.Sprintf("エラー: %v\n", err))
		os.Exit(1)
	}

	printLocalized(
		fmt.Sprintf("Line diff for %s between commits [%s] and [%s]:\n", fileName, fromCommitID, toCommitID),
		fmt.Sprintf("ファイル %s のコミット [%s] と [%s] の行差分:\n", fileName, fromCommitID, toCommitID),
	)
	printLineDiff(fromLines, toLines)
}

func runRevert(commitID string) {
	ensureInitialized()
	targetPath, err := materializeCommit(commitID)
	if err != nil {
		printLocalized(
			fmt.Sprintf("Error: commit ID '%s' not found.\n", commitID),
			fmt.Sprintf("エラー: コミットID '%s' が見つかりません。\n", commitID),
		)
		os.Exit(1)
	}
	defer os.RemoveAll(targetPath)

	// 現在の作業ディレクトリをクリーンアップ
	filepath.Walk(".", func(path string, info os.FileInfo, err error) error {
		if path == "." || shouldIgnore(path, []string{LitDir, ".git", ".DS_Store"}) {
			return nil
		}
		os.RemoveAll(path)
		return nil
	})

	// スナップショットから復元
	err = copyDirectory(targetPath, ".", []string{})
	if err != nil {
		printLocalized(fmt.Sprintf("Restore failed: %v\n", err), fmt.Sprintf("復元に失敗しました: %v\n", err))
		os.Exit(1)
	}

	printLocalized(fmt.Sprintf("Restored to commit [%s].\n", commitID), fmt.Sprintf("コミット [%s] の状態に復元しました。\n", commitID))
}

func runSetLanguage(lang string) {
	ensureInitialized()
	config := Config{}
	loadJSON(ConfigFile, &config)
	config.Language = lang
	if config.MaxVersions == 0 {
		config.MaxVersions = 10
	}
	saveJSON(ConfigFile, config)
	if lang == LanguageJP {
		fmt.Println("Output language set to Japanese.")
		return
	}
	fmt.Println("Output language set to English.")
}

func buildCommitPayload(parentID string, full bool, targets []string) ([]string, []string, error) {
	currentFiles := map[string]string{}
	if err := collectCurrentFiles(".", currentFiles); err != nil {
		return nil, nil, err
	}

	parentFiles := map[string]string{}
	if !full && parentID != "" {
		parentPath, err := materializeCommit(parentID)
		if err != nil {
			return nil, nil, err
		}
		defer os.RemoveAll(parentPath)
		if err := collectCurrentFiles(parentPath, parentFiles); err != nil {
			return nil, nil, err
		}
	}

	targetSet := map[string]bool{}
	for _, target := range targets {
		target = filepath.Clean(target)
		if target == "." || target == string(filepath.Separator) {
			continue
		}
		targetSet[target] = true
	}

	if len(targetSet) > 0 {
		for target := range targetSet {
			if _, ok := currentFiles[target]; ok {
				continue
			}
			if _, ok := parentFiles[target]; ok {
				continue
			}
			return nil, nil, fmt.Errorf("target file not found: %s", target)
		}
	}

	var changed []string
	var deleted []string

	for path, hash := range currentFiles {
		if len(targetSet) > 0 && !targetSet[path] {
			continue
		}
		if parentHash, ok := parentFiles[path]; !ok || parentHash != hash || full {
			changed = append(changed, path)
		}
	}
	for path := range parentFiles {
		if len(targetSet) > 0 && !targetSet[path] {
			continue
		}
		if _, ok := currentFiles[path]; !ok {
			deleted = append(deleted, path)
		}
	}
	return changed, deleted, nil
}

func collectCurrentFiles(root string, files map[string]string) error {
	type fileJob struct {
		relPath string
		path    string
	}

	var jobs []fileJob
	if err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		relPath, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if relPath == "." || shouldIgnore(relPath, []string{LitDir, ".git", ".DS_Store", ".litignore"}) {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if info.IsDir() {
			return nil
		}
		jobs = append(jobs, fileJob{relPath: relPath, path: path})
		return nil
	}); err != nil {
		return err
	}

	if len(jobs) == 0 {
		return nil
	}

	workerCount := runtime.NumCPU()
	if workerCount < 2 {
		workerCount = 2
	}
	jobCh := make(chan fileJob, len(jobs))
	errCh := make(chan error, 1)
	var wg sync.WaitGroup
	var mu sync.Mutex

	worker := func() {
		defer wg.Done()
		for job := range jobCh {
			hash := hashFile(job.path)
			if hash == "" {
				select {
				case errCh <- fmt.Errorf("failed to hash file: %s", job.path):
				default:
				}
				return
			}
			mu.Lock()
			files[job.relPath] = hash
			mu.Unlock()
		}
	}

	wg.Add(workerCount)
	for i := 0; i < workerCount; i++ {
		go worker()
	}

	for _, job := range jobs {
		select {
		case err := <-errCh:
			close(jobCh)
			wg.Wait()
			return err
		default:
			jobCh <- job
		}
	}
	close(jobCh)
	wg.Wait()

	select {
	case err := <-errCh:
		return err
	default:
		return nil
	}
}

func materializeCommit(commitID string) (string, error) {
	var history []Commit
	loadJSON(HistoryFile, &history)
	target := findCommit(commitID, history)
	if target == nil {
		return "", fmt.Errorf("commit not found")
	}

	tmpDir, err := os.MkdirTemp("", "lit-commit-*")
	if err != nil {
		return "", err
	}
	if err := restoreCommitInto(tmpDir, *target, history); err != nil {
		os.RemoveAll(tmpDir)
		return "", err
	}
	return tmpDir, nil
}

func restoreCommitInto(dst string, commit Commit, history []Commit) error {
	if commit.ParentID != "" {
		parent := findCommit(commit.ParentID, history)
		if parent == nil {
			return fmt.Errorf("missing parent")
		}
		if err := restoreCommitInto(dst, *parent, history); err != nil {
			return err
		}
	} else {
		if err := os.RemoveAll(dst); err != nil {
			return err
		}
		if err := os.MkdirAll(dst, 0o755); err != nil {
			return err
		}
	}

	commitPath := filepath.Join(ObjectsDir, commit.ID)
	if err := applyCommitFiles(commitPath, dst); err != nil {
		return err
	}
	for _, deleted := range commit.Deleted {
		os.RemoveAll(filepath.Join(dst, deleted))
	}
	return nil
}

func applyCommitFiles(commitPath, dst string) error {
	return filepath.Walk(commitPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || filepath.Base(path) == "meta.json" {
			return nil
		}
		relPath, err := filepath.Rel(commitPath, path)
		if err != nil {
			return err
		}
		dstPath := filepath.Join(dst, relPath)
		if err := os.MkdirAll(filepath.Dir(dstPath), 0o755); err != nil {
			return err
		}
		return copyFile(path, dstPath)
	})
}

func findCommit(commitID string, history []Commit) *Commit {
	for i := range history {
		if history[i].ID == commitID {
			return &history[i]
		}
	}
	return nil
}

func promoteCommitToFull(commitID string) {
	var history []Commit
	loadJSON(HistoryFile, &history)
	commit := findCommit(commitID, history)
	if commit == nil || commit.Full {
		return
	}

	tmpDir, err := materializeCommit(commitID)
	if err != nil {
		return
	}
	defer os.RemoveAll(tmpDir)

	commitPath := filepath.Join(ObjectsDir, commitID)
	_ = os.RemoveAll(commitPath)
	_ = os.MkdirAll(commitPath, 0o755)
	_ = copyDirectory(tmpDir, commitPath, nil)
	commit.Full = true
	commit.ParentID = ""
	commit.Deleted = nil
	saveJSON(filepath.Join(commitPath, "meta.json"), commit)
}

func readLines(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	text = strings.TrimSuffix(text, "\n")
	if text == "" {
		return []string{}, nil
	}
	return strings.Split(text, "\n"), nil
}

func isCommitID(value string) bool {
	var history []Commit
	loadJSON(HistoryFile, &history)
	return findCommit(value, history) != nil
}

func printLineDiff(current, target []string) {
	maxLen := len(current)
	if len(target) > maxLen {
		maxLen = len(target)
	}

	for i := 0; i < maxLen; i++ {
		switch {
		case i >= len(current):
			fmt.Printf("+ %s\n", target[i])
		case i >= len(target):
			fmt.Printf("- %s\n", current[i])
		case current[i] == target[i]:
			fmt.Printf("  %s\n", current[i])
		default:
			fmt.Printf("- %s\n", current[i])
			fmt.Printf("+ %s\n", target[i])
		}
	}
}

// --- ユーティリティ関数 ---

func isJapanese() bool {
	config := Config{}
	loadJSON(ConfigFile, &config)
	return config.Language == LanguageJP
}

func printLocalized(en, jp string) {
	if isJapanese() {
		fmt.Print(jp)
		return
	}
	fmt.Print(en)
}

func ensureInitialized() {
	if _, err := os.Stat(LitDir); os.IsNotExist(err) {
		printLocalized("Error: Lit repository is not initialized. Run 'lit init' first.\n", "エラー: Lit リポジトリが初期化されていません。'lit init' を実行してください。\n")
		os.Exit(1)
	}
}

func saveJSON(filename string, data interface{}) {
	file, _ := os.Create(filename)
	defer file.Close()
	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	encoder.Encode(data)
}

func loadJSON(filename string, data interface{}) {
	file, err := os.Open(filename)
	if err == nil {
		defer file.Close()
		json.NewDecoder(file).Decode(data)
	}
}

func projectDBPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ProjectsDB
	}
	return filepath.Join(home, ProjectsDB)
}

func loadProjects() []ProjectEntry {
	var projects []ProjectEntry
	loadJSON(projectDBPath(), &projects)
	return projects
}

func registerProject() {
	cwd, err := os.Getwd()
	if err != nil {
		return
	}

	projects := loadProjects()
	for _, p := range projects {
		if p.Path == cwd {
			return
		}
	}

	projects = append([]ProjectEntry{{
		Name:      filepath.Base(cwd),
		Path:      cwd,
		CreatedAt: time.Now(),
	}}, projects...)
	saveJSON(projectDBPath(), projects)
}

func copyDirectory(src, dst string, ignore []string) error {
	type copyJob struct {
		src string
		dst string
	}

	var jobs []copyJob
	if err := filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		relPath, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if relPath == "." || shouldIgnore(relPath, ignore) {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		dstPath := filepath.Join(dst, relPath)
		if info.IsDir() {
			return os.MkdirAll(dstPath, info.Mode())
		}

		jobs = append(jobs, copyJob{src: path, dst: dstPath})
		return nil
	}); err != nil {
		return err
	}

	if len(jobs) == 0 {
		return nil
	}

	workerCount := runtime.NumCPU()
	if workerCount < 2 {
		workerCount = 2
	}
	jobCh := make(chan copyJob, len(jobs))
	errCh := make(chan error, 1)
	var wg sync.WaitGroup

	worker := func() {
		defer wg.Done()
		for job := range jobCh {
			if err := copyFile(job.src, job.dst); err != nil {
				select {
				case errCh <- err:
				default:
				}
				return
			}
		}
	}

	wg.Add(workerCount)
	for i := 0; i < workerCount; i++ {
		go worker()
	}

	for _, job := range jobs {
		select {
		case err := <-errCh:
			close(jobCh)
			wg.Wait()
			return err
		default:
			jobCh <- job
		}
	}
	close(jobCh)
	wg.Wait()

	select {
	case err := <-errCh:
		return err
	default:
		return nil
	}
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}

func shouldIgnore(path string, ignoreList []string) bool {
	path = filepath.Clean(path)
	parts := strings.Split(path, string(filepath.Separator))
	for _, ignore := range ignoreList {
		for _, part := range parts {
			if part == ignore {
				return true
			}
		}
	}
	return false
}

func hashFile(filename string) string {
	f, err := os.Open(filename)
	if err != nil {
		return ""
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return ""
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}
