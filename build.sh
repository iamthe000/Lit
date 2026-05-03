#!/bin/bash

APP_NAME="lit"

echo "Building for Windows..."
GOOS=windows GOARCH=amd64 go build -o bin/${APP_NAME}.exe main.go

echo "Building for Linux..."
GOOS=linux GOARCH=amd64 go build -o bin/${APP_NAME}-linux main.go

echo "Building for macOS (M1/M2/M3)..."
GOOS=darwin GOARCH=arm64 go build -o bin/${APP_NAME}-mac-arm main.go

echo "Building for macOS (Intel)..."
GOOS=darwin GOARCH=amd64 go build -o bin/${APP_NAME}-mac-intel main.go

echo "Done! Check the 'bin' directory."
