package main

import (
	"encoding/json"
	"fmt"
	"github.com/Varjelus/dirsync"
	"github.com/disintegration/imaging"
	"html/template"
	"image"
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
}

type thumbnailConfig struct {
	Method string
	Width  int
	Height int
}

type dirConfig map[string]fileConfig

type fileConfig struct {
	Template      string
	Data          interface{}
	AutoThumbnail map[string]thumbnailConfig
}

var Config config
var DefaultDirConfig = make(map[string]fileConfig)

var InputPath = filepath.Dir(os.Args[0])

const Port = 8080

const StaticDirName = "static"
const SourceDirName = "src"
const TemplateDirName = "templates"
const DirConfigFileName = "siteware.json"
const DefaultTemplateName = "default.template"
const ConfigFileName = "siteware.master.json"
const ThumbDirName = ".thumbs"

var InfoLogger = log.New(os.Stdout, "# ", log.Lmicroseconds)
var ErrorLogger = log.New(os.Stdout, "Error: ", log.Lmicroseconds)

var Commands = make(map[string]command)

var TemplateFunctions template.FuncMap

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

	TemplateFunctions = template.FuncMap{
		"readdir": readdir,
	}
}

func main() {
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
	InfoLogger.Printf("Serving files at http://localhost:%d. Press Ctrl+C to terminate.\n", Port)
	ErrorLogger.Fatalln(http.ListenAndServe(fmt.Sprintf(":%d", Port), http.FileServer(http.Dir(InputPath))))
}

func initialize() {
	InfoLogger.Println("Initializing new project...")
	fi, err := os.Stat(InputPath)
	if err != nil {
		ErrorLogger.Fatalf("Error reading parent directory info: %v\n", err)
	}
	if err := os.MkdirAll(filepath.Join(InputPath, StaticDirName), fi.Mode()); err != nil {
		ErrorLogger.Fatalf("Error creating static directory: %v\n", err)
	}
	if err := os.MkdirAll(filepath.Join(InputPath, SourceDirName), fi.Mode()); err != nil {
		ErrorLogger.Fatalf("Error creating source directory: %v\n", err)
	}
	if err := os.MkdirAll(filepath.Join(InputPath, TemplateDirName), fi.Mode()); err != nil {
		ErrorLogger.Fatalf("Error creating template directory: %v\n", err)
	}
	InfoLogger.Println("Done!")
}

func build() {
	// Load config
	cfgPath := filepath.Join(InputPath, ConfigFileName)
	cfgf, err := os.Open(cfgPath)
	if err != nil {
		ErrorLogger.Fatalf("Error opening config file \"%s\": %v\n", cfgPath, err)
	}
	if err := json.NewDecoder(cfgf).Decode(&Config); err != nil {
		ErrorLogger.Fatalf("Error decoding config file \"%s\": %v\n", cfgPath, err)
	}
	if err := cfgf.Close(); err != nil {
		ErrorLogger.Fatalf("Error closing config file \"%s\": %v\n", cfgPath, err)
	}

	InputPath, err := filepath.Abs(InputPath)
	if err != nil {
		ErrorLogger.Fatalf("Error resolving input path %s: %v\n", InputPath, err)
	}
	if Config.Output == "" {
		ErrorLogger.Fatalln("Output directory unset in configuration")
	}

	// Clear site repo, excluding .git and static files directory
	InfoLogger.Println("Clearing output repo...")
	repo, err := os.Open(Config.Output)
	if err != nil {
		if os.IsNotExist(err) {
			ErrorLogger.Fatalf("Path %s does not exist\n", Config.Output)
		}

		ErrorLogger.Fatalf("Can't open path %s: %v\n", Config.Output, err)
	}
	files, err := repo.Readdir(0)
	if err != nil {
		panic(err)
	}
	for _, file := range files {
		if file.Name() == ".git" || file.Name() == StaticDirName || file.Name() == ".gitignore" || file.Name() == "CNAME" {
			continue
		}
		if file.IsDir() {
			os.RemoveAll(filepath.Join(Config.Output, file.Name()))
		} else {
			os.Remove(filepath.Join(Config.Output, file.Name()))
		}
	}
	if err := repo.Close(); err != nil {
		ErrorLogger.Fatalf("Error closing destination: %v\n", err)
	}

	// Sync static files
	InfoLogger.Println("Syncing statics...")
	if err := dirsync.Sync(filepath.Join(InputPath, StaticDirName), filepath.Join(Config.Output, StaticDirName)); err != nil {
		ErrorLogger.Fatalf("Error syncing static files: %v\n", err)
	}

	// Generate HTML
	InfoLogger.Println("Generating HTML files...")
	if err := generateHTML(); err != nil {
		ErrorLogger.Fatalf("Error generating HTML: %v\n", err)
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
					//InfoLogger.Println("Using default configuration")
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

				// FIXME: This functionality should be moved elsewhere
				// Generate thumbnails
				if _, exist := cfg[StaticDirName]; exist {
					for imgDirPath, thumbCfg := range cfg[StaticDirName].AutoThumbnail {
						imgSrcDirPath := filepath.Join(InputPath, StaticDirName, imgDirPath)
						InfoLogger.Printf("Generating thumbnails for %s...\n", imgDirPath)
						if err := os.MkdirAll(filepath.Join(Config.Output, StaticDirName, imgDirPath, ThumbDirName), info.Mode()); err != nil {
							return err
						}
						if err := filepath.Walk(imgSrcDirPath, func(imgPath string, imgInfo os.FileInfo, err error) error {
							ext := filepath.Ext(imgPath)
							if ext != ".png" && ext != ".jpg" && ext != ".jpeg" {
								return nil
							}
							relImgPath := strings.TrimPrefix(imgPath, filepath.Join(InputPath, SourceDirName))
							destImgPath := filepath.Join(Config.Output, filepath.Dir(relImgPath), ThumbDirName, imgInfo.Name())
							if err := thumbnail(imgPath, destImgPath, thumbCfg); err != nil {
								return err
							}
							return nil
						}); err != nil {
							return err
						}
					}
				}
			}
		}
		var ftmpl string
		var fdata interface{}
		fcfg, exist := cfg[info.Name()]
		if !exist {
			ftmpl = DefaultTemplateName
			fdata = nil
		} else {
			if fcfg.Template == "" {
				ftmpl = DefaultTemplateName
			} else {
				ftmpl = fcfg.Template
			}
			fdata = fcfg.Data
		}
		//InfoLogger.Printf("Using configuration %v for %s\n", fdata, path)

		ext := filepath.Ext(path)
		if info.Mode().IsDir() {
			//InfoLogger.Printf("Creating directory %s...\n", relPath)
			return os.MkdirAll(destPath, info.Mode())
		} else if info.Mode().IsRegular() && ext == ".html" || ext == ".htm" {
			//InfoLogger.Printf("Create %s\n", relPath)

			// Create file
			file, err := os.Create(destPath)
			if err != nil {
				return err
			}

			// Run templates
			t, err := template.New(ftmpl).Funcs(TemplateFunctions).ParseFiles(filepath.Join(InputPath, TemplateDirName, ftmpl), path)
			if err != nil {
				return err
			}
			if err := t.Execute(file, fdata); err != nil {
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

func thumbnail(src string, dest string, cfg thumbnailConfig) error {
	srcImg, err := imaging.Open(src)
	if err != nil {
		return err
	}

	var thumb *image.NRGBA

	switch strings.ToLower(cfg.Method) {
	case "resize":
		thumb = imaging.Resize(srcImg, cfg.Width, cfg.Height, imaging.Box)
	case "fit":
		thumb = imaging.Fit(srcImg, cfg.Width, cfg.Height, imaging.Box)
	case "fill":
		thumb = imaging.Fill(srcImg, cfg.Width, cfg.Height, imaging.Center, imaging.Box)
	case "thumbnail":
		fallthrough
	default:
		thumb = imaging.Thumbnail(srcImg, cfg.Width, cfg.Height, imaging.Box)
	}

	if err = imaging.Save(thumb, dest); err != nil {
		return err
	}

	return nil
}

func readdir(path string) (files []os.FileInfo) {
	var err error

	path, err = filepath.Abs(path)
	if err != nil {
		return
	}

	var root *os.File
	root, err = os.Open(path)
	if err != nil {
		return
	}
	defer root.Close()

	files, err = root.Readdir(0)
	return
}
