package main

import (
    "log"
    "os"
    "strings"
    "flag"
    "fmt"
    "net/http"
    "html/template"
    "path/filepath"
    "github.com/Varjelus/dirsync"
)

type command struct {
    F func()
    Description string
}

var InputPath = flag.String("i", filepath.Dir(os.Args[0]), "Source files path")
var OutputPath = flag.String("o", "", "Output path")
var Port = flag.Int("p", 8080, "HTTP server port number")

const StaticDirName = "static"
const SourceDirName = "src"
const TemplateDirName = "templates"

var InfoLogger = log.New(os.Stdout, "# ", log.Lmicroseconds)
var ErrorLogger = log.New(os.Stdout, "Error: ", log.Lmicroseconds)

var Commands = make(map[string]command)

func init() {
    flag.Parse()

    Commands["init"] = command{
        F: initialize,
        Description: "Initializes a new empty project at current directory.",
    }
    Commands["build"] = command{
        F: build,
        Description: "Builds files from -i to -o.",
    }
    Commands["serve"] = command{
        F: serve,
        Description: "Serves current directory with HTTP. Set port with -p.",
    }
}

func main() {
    if len(os.Args) < 2 {
        fmt.Println("Please provide a command")
        flag.PrintDefaults()
        os.Exit(1)
    }
    cmdStr := os.Args[1]
    cmd, exist := Commands[cmdStr]
    if !exist {
        fmt.Printf("Unknown command \"%s\".\n", cmdStr)
        for c := range Commands {
            fmt.Printf("%s:\t %s\n", c, Commands[c].Description)
        }
        os.Exit(1)
    }
    cmd.F()
}

func serve() {
    InfoLogger.Printf("Serving files at http://localhost:%d...\n", *Port)
    ErrorLogger.Println(http.ListenAndServe(fmt.Sprintf(":%d", *Port), http.FileServer(http.Dir(os.Args[0]))))
}

func initialize() {
    InfoLogger.Println("Initializing new project...")
    fi, err := os.Stat(filepath.Dir(os.Args[0]))
    if err != nil {
        ErrorLogger.Printf("Error reading parent directory info: %v\n", err)
        os.Exit(1)
    }
    if err := os.MkdirAll(filepath.Join(os.Args[0], StaticDirName), fi.Mode()); err != nil {
        ErrorLogger.Printf("Error creating static directory: %v\n", err)
        os.Exit(1)
    }
    if err := os.MkdirAll(filepath.Join(os.Args[0], SourceDirName), fi.Mode()); err != nil {
        ErrorLogger.Printf("Error creating source directory: %v\n", err)
        os.Exit(1)
    }
    if err := os.MkdirAll(filepath.Join(os.Args[0], TemplateDirName), fi.Mode()); err != nil {
        ErrorLogger.Printf("Error creating template directory: %v\n", err)
        os.Exit(1)
    }
    InfoLogger.Println("Done!")
}

func build() {
    InputPath, err := filepath.Abs(*InputPath)
    if err != nil {
        ErrorLogger.Printf("Error resolving input path %s: %v\n", InputPath, err)
        os.Exit(1)
    }
    if *OutputPath == "" {
        ErrorLogger.Println("Output directory unset")
        flag.PrintDefaults()
        os.Exit(1)
    }

    // Clear site repo, excluding .git and static files directory
    InfoLogger.Println("Clearing output repo...")
    repo, err := os.Open(*OutputPath)
    if err != nil {
        if os.IsNotExist(err) {
            ErrorLogger.Printf("Path %s does not exist\n", *OutputPath)
        } else {
            ErrorLogger.Printf("Can't open path %s: %v\n", *OutputPath, err)
        }
        os.Exit(1)
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
            os.RemoveAll(filepath.Join(*OutputPath, file.Name()))
        } else {
            os.Remove(filepath.Join(*OutputPath, file.Name()))
        }
    }
    if err := repo.Close(); err != nil {
        ErrorLogger.Printf("Error closing destination: %v\n", err)
        os.Exit(1)
    }

    // Sync static files
    InfoLogger.Println("Syncing statics...")
    if err := dirsync.Sync(filepath.Join(InputPath, StaticDirName), filepath.Join(*OutputPath, StaticDirName)); err != nil {
        ErrorLogger.Printf("Error syncing static files: %v\n", err)
        os.Exit(1)
    }

    // Generate HTML
    InfoLogger.Println("Generating HTML files...")
    if err := generateHTML(); err != nil {
        ErrorLogger.Printf("Error generating HTML: %v\n", err)
        os.Exit(1)
    }
}

func generateHTML() error {
    if err := filepath.Walk(filepath.Join(*InputPath, SourceDirName), func(path string, info os.FileInfo, err error) error {
        relPath := strings.TrimPrefix(path, filepath.Join(*InputPath, SourceDirName))
        destPath := filepath.Join(*OutputPath, relPath)
        if err != nil {
            return err
        }

        if info.Mode().IsDir() {
            InfoLogger.Printf("Creating directory \\%s...\n", relPath)
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
                filepath.Join(*InputPath, TemplateDirName, "base.tmpl"),
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
