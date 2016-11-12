package main

import (
	"encoding/json"
	"fmt"
	"github.com/Varjelus/dirsync"
	"html/template"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

type command struct {
	F           func()
	Description string
}

type config struct {
	Output string
	Port   int
}

type dirConfig map[string]fileConfig

type fileConfig struct {
	Template string
	Data     interface{}
}

var Config config
var DefaultDirConfig = make(map[string]fileConfig)

var InputPath = filepath.Dir(os.Args[0])

const StaticDirName = "static"
const SourceDirName = "src"
const TemplateDirName = "templates"
const DirConfigFileName = "siteware.json"
const DefaultTemplateName = "default.template"
const ConfigFileName = "siteware.master.json"

var InfoLogger = log.New(os.Stdout, "# ", log.Lmicroseconds)
var ErrorLogger = log.New(os.Stdout, "Error: ", log.Lmicroseconds)

var Commands = make(map[string]command)

func init() {
	Commands["init"] = command{
		F:           initialize,
		Description: "Initializes a new empty project at current directory.",
	}
	Commands["build"] = command{
		F:           build,
		Description: "Builds files from current directory to the one specified in configuration.",
	}
	Commands["serve"] = command{
		F:           serve,
		Description: "Serves current directory with HTTP.",
	}
}

func main() {
	// Load config
	cfgPath := filepath.Join(InputPath, ConfigFileName)
	cfgf, err := os.Open(cfgPath)
	if err != nil {
		ErrorLogger.Printf("Error opening config file \"%s\": %v\n", cfgPath, err)
		os.Exit(1)
	}
	if err := json.NewDecoder(cfgf).Decode(&Config); err != nil {
		ErrorLogger.Printf("Error decoding config file \"%s\": %v\n", cfgPath, err)
		os.Exit(1)
	}
	if err := cfgf.Close(); err != nil {
		ErrorLogger.Printf("Error closing config file \"%s\": %v\n", cfgPath, err)
		os.Exit(1)
	}

	// Run a command
	if len(os.Args) < 2 {
		fmt.Println("Please provide a command")
        for c := range Commands {
			fmt.Printf("%s:\t %s\n", c, Commands[c].Description)
		}
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
	InfoLogger.Printf("Serving files at http://localhost:%d...\n", Config.Port)
	ErrorLogger.Println(http.ListenAndServe(fmt.Sprintf(":%d", Config.Port), http.FileServer(http.Dir(InputPath))))
}

func initialize() {
	InfoLogger.Println("Initializing new project...")
	fi, err := os.Stat(InputPath)
	if err != nil {
		ErrorLogger.Printf("Error reading parent directory info: %v\n", err)
		os.Exit(1)
	}
	if err := os.MkdirAll(filepath.Join(InputPath, StaticDirName), fi.Mode()); err != nil {
		ErrorLogger.Printf("Error creating static directory: %v\n", err)
		os.Exit(1)
	}
	if err := os.MkdirAll(filepath.Join(InputPath, SourceDirName), fi.Mode()); err != nil {
		ErrorLogger.Printf("Error creating source directory: %v\n", err)
		os.Exit(1)
	}
	if err := os.MkdirAll(filepath.Join(InputPath, TemplateDirName), fi.Mode()); err != nil {
		ErrorLogger.Printf("Error creating template directory: %v\n", err)
		os.Exit(1)
	}
	InfoLogger.Println("Done!")
}

func build() {
	InputPath, err := filepath.Abs(InputPath)
	if err != nil {
		ErrorLogger.Printf("Error resolving input path %s: %v\n", InputPath, err)
		os.Exit(1)
	}
	if Config.Output == "" {
		ErrorLogger.Println("Output directory unset in configuration")
		os.Exit(1)
	}

	// Clear site repo, excluding .git and static files directory
	InfoLogger.Println("Clearing output repo...")
	repo, err := os.Open(Config.Output)
	if err != nil {
		if os.IsNotExist(err) {
			ErrorLogger.Printf("Path %s does not exist\n", Config.Output)
		} else {
			ErrorLogger.Printf("Can't open path %s: %v\n", Config.Output, err)
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
			os.RemoveAll(filepath.Join(Config.Output, file.Name()))
		} else {
			os.Remove(filepath.Join(Config.Output, file.Name()))
		}
	}
	if err := repo.Close(); err != nil {
		ErrorLogger.Printf("Error closing destination: %v\n", err)
		os.Exit(1)
	}

	// Sync static files
	InfoLogger.Println("Syncing statics...")
	if err := dirsync.Sync(filepath.Join(InputPath, StaticDirName), filepath.Join(Config.Output, StaticDirName)); err != nil {
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
	configs := make(map[string]dirConfig)

	if err := filepath.Walk(filepath.Join(InputPath, SourceDirName), func(path string, info os.FileInfo, err error) error {
		relPath := strings.TrimPrefix(path, filepath.Join(InputPath, SourceDirName))
		destPath := filepath.Join(Config.Output, relPath)
		if err != nil {
			return err
		}

		// Try to load config file
        InfoLogger.Println("Reading directory configuration...")
		// Get path of this directory
		dir := filepath.Dir(path)
		// See if the config for this dir is already read
		cfg, exist := configs[dir]
		// If it is not
		if !exist {
			// Try to read it from file
			cfgf, err := os.Open(filepath.Join(dir, DirConfigFileName))
			if err != nil {
				// If there is no config file, use defaults
				if os.IsNotExist(err) {
					configs[dir] = DefaultDirConfig
					cfg = DefaultDirConfig
				} else {
                    return err
                }
			} else {
                // Decode the json and close the file
    			if err := json.NewDecoder(cfgf).Decode(&cfg); err != nil {
    				return err
    			}
    			configs[dir] = cfg
    			if err := cfgf.Close(); err != nil {
    				return err
    			}
            }
		}

		ext := filepath.Ext(path)
		if info.Mode().IsDir() {
			InfoLogger.Printf("Creating directory %s...\n", relPath)
			return os.MkdirAll(destPath, info.Mode())
		} else if info.Mode().IsRegular() && ext == ".html" || ext == ".htm" {
			InfoLogger.Printf("Creating %s...\n", relPath)

			// Create file
			file, err := os.Create(destPath)
			if err != nil {
				return err
			}

			// Run templates
			var ftmpl string
			var fdata interface{}
			fcfg, exist := cfg[info.Name()]
			if !exist {
				ftmpl = DefaultTemplateName
				fdata = nil
			} else {
				ftmpl = fcfg.Template
				fdata = fcfg.Data
			}

			if err := template.Must(template.ParseFiles(
				filepath.Join(InputPath, TemplateDirName, ftmpl),
				path,
			)).Execute(file, fdata); err != nil {
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
