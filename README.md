# Lit

Lit は、現在のディレクトリを簡易的な「プロジェクト」として管理し、変更をスナップショット保存できる Go 製の軽量バージョン管理ツールです。

`init` で初期化し、`commit` で状態を保存し、`log` / `diff` / `revert` で履歴を扱えます。表示言語は英語と日本語を切り替えられます。

## 特徴

* 現在の作業ディレクトリをプロジェクトとして初期化
* 変更を履歴として保存
* コミット履歴の表示
* コミットとの差分表示
* 指定コミットへの復元
* 英語 / 日本語の出力切り替え
* 初期化済みプロジェクト一覧の表示

## インストール

Go がインストールされていれば、`main.go` をそのままビルドできます。

```bash
go build -o lit main.go
```

または直接実行できます。

```bash
go run main.go <command>
```

## 使い方

### 初期化

現在のディレクトリを Lit リポジトリとして初期化します。

```bash
lit init
```

保存する最大世代数を指定することもできます。

```bash
lit init -max 20
```

### コミット

変更を保存します。

```bash
lit commit -m "first snapshot"
```

メッセージを省略すると `Auto commit` になります。

### 履歴表示

```bash
lit log
```

### 初期化済みプロジェクト一覧

```bash
lit ls
```

### 差分表示

現在の内容と指定コミットの差分を表示します。

```bash
lit diff <commit\_id>
```

ファイル単位で比較する場合は、コミット ID のあとにファイル名を指定します。

```bash
lit diff <commit\_id> path/to/file.txt
```

コミット同士を比較することもできます。

```bash
lit diff <from\_commit\_id> <to\_commit\_id>
```

さらに、コミット間の特定ファイルの行差分も確認できます。

```bash
lit diff <from\_commit\_id> <to\_commit\_id> path/to/file.txt
```

### 復元

指定コミットの状態に作業ディレクトリを戻します。

```bash
lit revert <commit\_id>
```

## 表示言語

出力言語を切り替えられます。

```bash
lit en
lit jp
```

## 保存されるもの

Lit は初期化したディレクトリ直下に `.lit/` を作成し、次の情報を保持します。

* `.lit/config.json`
* `.lit/history.json`
* `.lit/objects/`

また、ユーザーのホームディレクトリに `.lit-projects.json` を保存して、初期化済みプロジェクト一覧を管理します。

## 注意点

* このツールは Git の完全な代替ではなく、軽量なスナップショット管理を目的としています。
* コミット ID はタイムスタンプベースの簡易 ID です。
* `revert` は現在の作業ディレクトリを復元対象の内容で置き換えます。必要な変更がある場合は事前に退避してください。
* `.git` と `.DS\_Store` は差分計算や復元時の除外対象です。

## コマンド一覧

```text
lit init \[-max <n>]
lit commit -m "<msg>"
lit log
lit ls
lit diff <id>
lit revert <id>
lit en
lit jp
```

## ライセンス

このリポジトリにライセンスファイルがない場合は、必要に応じて追加してください。

