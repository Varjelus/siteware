package main

import (
    "log"
    "os"
    "strings"
    "html/template"
    "path/filepath"
    "github.com/Varjelus/dirsync"
)

var GoPath = os.Getenv("GOPATH")
const OrgName = "LivewareEsports"
const SourceRepoName = "liveware.fi"
const DestinationRepoName = "livewareesports.github.io"
const StaticDirName = "static"
const SourceDirName = "src"
const TemplateDirName = "templates"
var SourceRepo = filepath.Join(GoPath, OrgName, SourceRepoName)
var DestinationRepo = filepath.Join(GoPath, OrgName, DestinationRepoName)

var InfoLogger = log.New(os.Stdout, "# ", log.Lmicroseconds)

func init() {
    if GoPath == "" {
        panic("GOPATH not set")
    }
}

func main() {
    // Clear site repo, excluding .git and static files directory
    InfoLogger.Println("Clearing output repo...")
    repo, err := os.Open(DestinationRepo)
    if err != nil {
        panic(err)
    }
    files, err := repo.Readdir(0)
    if err != nil {
        panic(err)
    }
    for _, file := range files {
        if file.Name() == ".git" || file.Name() == StaticDirName {
            continue
        }
        if file.IsDir() {
            os.RemoveAll(filepath.Join(DestinationRepo, file.Name()))
        } else {
            os.Remove(filepath.Join(DestinationRepo, file.Name()))
        }
    }
    if err := repo.Close(); err != nil {
        panic(err)
    }

    // Sync static files
    InfoLogger.Println("Syncing statics...")
    if err := dirsync.Sync(filepath.Join(SourceRepo, StaticDirName), filepath.Join(DestinationRepo, StaticDirName)); err != nil {
        panic(err)
    }

    // Generate HTML
    InfoLogger.Println("Generating HTML files...")
    if err := generateHTML(); err != nil {
        panic(err)
    }
}

func generateHTML() error {
    if err := filepath.Walk(filepath.Join(SourceRepo, SourceDirName), func(path string, info os.FileInfo, err error) error {
        relPath := strings.TrimPrefix(path, filepath.Join(SourceRepo, SourceDirName))
        destPath := filepath.Join(DestinationRepo, relPath)
        if err != nil {
            return err
        }

        if info.Mode().IsDir() {
            InfoLogger.Printf("Creating directory %s...\n", relPath)
            return os.MkdirAll(destPath, info.Mode())
        } else if info.Mode().IsRegular() {
            InfoLogger.Printf("Creating %s...\n", relPath)
            // Create file
            file, err := os.Create(destPath)
            if err != nil {
                return err
            }
            // Run templates
            if err := template.Must(template.ParseFiles(
                filepath.Join(SourceRepo, TemplateDirName, "base.tmpl"),
                path,
            )).Execute(file, nil); err != nil {
                return err
            }
            if err := file.Close(); err != nil {
                return err
            }
        }
        return nil
    }); err != nil {
        return err
    }

    return nil
}
