package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/spf13/cobra"
	"oxen/generator"
	"oxen/server"
)

func buildSite(root string, workers int, forceRebuild bool, destDir string, siteName string) error {
	absDestDir, err := filepath.Abs(destDir)
	if err != nil {
		return fmt.Errorf("error getting absolute path for destDir: %w", err)
	}

	if err := os.MkdirAll(absDestDir, 0755); err != nil {
		return fmt.Errorf("error creating dest directory: %w", err)
	}

	if !forceRebuild {
		entries, err := os.ReadDir(absDestDir)
		if err != nil {
			if os.IsNotExist(err) {
				forceRebuild = true
			}
		} else if len(entries) == 0 {
			forceRebuild = true
		}
	}

	absPath, err := filepath.Abs(root)
	if err != nil {
		return fmt.Errorf("error getting absolute path: %w", err)
	}

	ctx := generator.BuildContext{
		Root:         absPath,
		DestDir:      absDestDir,
		Workers:      workers,
		ForceRebuild: forceRebuild,
		SiteName:     siteName,
	}

	startTime := time.Now()
	procFiles, phase1Result := generator.GetAndProcessOrgFiles(absPath, workers)

	pageTmpl, tagTmpl, indexTmpl, tmplModTime, err := generator.SetupTemplates(absPath)
	if err != nil {
		return err
	}
	ctx.TmplModTime = tmplModTime

	result := phase1Result
	result = result.Add(generator.GenerateHtmlPages(procFiles, ctx, pageTmpl))
	result = result.Add(generator.GenerateTagPages(procFiles, ctx, tagTmpl))
	result = result.Add(generator.GenerateIndexPage(procFiles, ctx, indexTmpl))
	result = result.Add(generator.CopyStaticFiles(ctx))

	result.SetStartTime(startTime)
	result.PrintSummary(procFiles)

	if srv != nil {
		srv.NotifyReload()
	}

	return nil
}

func runWatchMode(root string, workers int, forceRebuild bool, destDir string, siteName string) error {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("failed to create watcher: %w", err)
	}
	defer watcher.Close()

	// Run initial build
	fmt.Println("Starting initial build...")
	if err := buildSite(root, workers, forceRebuild, destDir, siteName); err != nil {
		log.Printf("Initial build failed: %v", err)
	}

	absPath, err := filepath.Abs(root)
	if err != nil {
		return fmt.Errorf("error getting absolute path: %w", err)
	}

	// Add the root directory
	if err := watcher.Add(absPath); err != nil {
		return fmt.Errorf("failed to watch root directory: %w", err)
	}

	// Walk directory tree and add all subdirectories to watcher
	destDirName := filepath.Base(destDir)
	err = filepath.WalkDir(absPath, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && d.Name() != destDirName {
			if err := watcher.Add(path); err != nil {
				log.Printf("Warning: failed to watch directory %s: %v", path, err)
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to walk directories: %w", err)
	}

	// Channel to signal rebuilds
	rebuildChan := make(chan bool, 1)
	hasPendingRebuild := false

	// Process events in a goroutine
	go func() {
		for {
			select {
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}

				// Only interested in create, write, remove, rename
				if event.Op&(fsnotify.Create|fsnotify.Write|fsnotify.Remove|fsnotify.Rename) == 0 {
					continue
				}

				// Skip anything in public directory
				if strings.HasPrefix(event.Name, destDir) {
					continue
				}

				// If a new directory is created, watch it
				if event.Op&fsnotify.Create == fsnotify.Create {
					if info, err := os.Stat(event.Name); err == nil && info.IsDir() {
						if err := watcher.Add(event.Name); err != nil {
							log.Printf("Warning: failed to watch new directory %s: %v", event.Name, err)
						}
					}
				}

				// Print watcher event for debugging
				log.Printf("Watcher event: %v %s", event.Op, event.Name)

				// Trigger rebuild
				if !hasPendingRebuild {
					hasPendingRebuild = true
					select {
					case rebuildChan <- true:
					default:
					}
				}

			case err, ok := <-watcher.Errors:
				if !ok {
					return
				}
				log.Printf("Watcher error: %v", err)
			}
		}
	}()

	fmt.Printf("Watching %s for changes... (Press Ctrl+C to stop)\n", absPath)

	// Main watch loop
	for {
		<-rebuildChan
		hasPendingRebuild = false

		// Debounce: wait a bit for more changes
		time.Sleep(100 * time.Millisecond)

		// Drain any additional rebuild signals
		select {
		case <-rebuildChan:
		default:
		}

		fmt.Println("\nChanges detected, rebuilding...")
		if err := buildSite(root, workers, forceRebuild, destDir, siteName); err != nil {
			log.Printf("Build failed: %v", err)
		}
		fmt.Printf("\nWatching %s for changes... (Press Ctrl+C to stop)\n", absPath)
	}
}

var (
	dir      string
	force    bool
	watch    bool
	workers  int
	port     int
	dest     string
	siteName string
	srv      *server.Server
)

func main() {
	var rootCmd = &cobra.Command{Use: "oxen"}

	var buildCmd = &cobra.Command{
		Use:   "build <dir>",
		Short: "Build the site from <dir>",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			if watch {
				if err := runWatchMode(args[0], workers, force, dest, siteName); err != nil {
					log.Fatalf("Watch mode failed: %v", err)
				}
			} else {
				if err := buildSite(args[0], workers, force, dest, siteName); err != nil {
					log.Fatalf("Build failed: %v", err)
				}
			}
		},
	}

	var serveCmd = &cobra.Command{
		Use:   "serve <dir>",
		Short: "Serve the built site",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			srv = server.NewServer(dest, port)
			go func() {
				if err := srv.Run(); err != nil {
					log.Printf("HTTP server error: %v", err)
				}
			}()

			if watch {
				if err := runWatchMode(args[0], workers, force, dest, siteName); err != nil {
					log.Fatalf("Watch mode failed: %v", err)
				}
			} else {
				if err := buildSite(args[0], workers, force, dest, siteName); err != nil {
					log.Fatalf("Build failed: %v", err)
				}
				fmt.Printf("\nServer running at http://localhost:%d\n", port)
				select {}
			}
		},
	}

	var lookupCmd = &cobra.Command{
		Use:   "lookup-id <dir> <id>",
		Short: "Find the file containing the given ID",
		Args:  cobra.ExactArgs(2),
		Run: func(cmd *cobra.Command, args []string) {
			absPath, err := filepath.Abs(args[0])
			if err != nil {
				log.Fatalf("Error getting absolute path: %v", err)
			}

			procFiles, _ := generator.GetAndProcessOrgFiles(absPath, workers)

			if path, found := procFiles.UuidMap.Load("id:" + args[1]); found {
				fmt.Printf("ID %s found in: %s\n", args[1], path.(string))
			} else {
				fmt.Printf("ID %s not found\n", args[1])
			}
		},
	}

	buildCmd.Flags().BoolVarP(&force, "force", "f", false, "force rebuild all files")
	buildCmd.Flags().BoolVarP(&watch, "watch", "w", false, "watch for changes and rebuild")
	buildCmd.Flags().IntVarP(&workers, "workers", "j", 8, "number of concurrent workers")
	buildCmd.Flags().StringVar(&dest, "dest", "public", "output directory")
	buildCmd.Flags().StringVar(&siteName, "site-name", "Neon Vagabond", "site name")

	serveCmd.Flags().BoolVarP(&force, "force", "f", false, "force rebuild all files")
	serveCmd.Flags().BoolVarP(&watch, "watch", "w", false, "watch for changes and rebuild")
	serveCmd.Flags().IntVarP(&workers, "workers", "j", 8, "number of concurrent workers")
	serveCmd.Flags().IntVarP(&port, "port", "p", 8080, "port to serve on")
	serveCmd.Flags().StringVar(&dest, "dest", "public", "output directory")
	serveCmd.Flags().StringVar(&siteName, "site-name", "Neon Vagabond", "site name")

	lookupCmd.Flags().IntVarP(&workers, "workers", "j", 8, "number of concurrent workers")

	rootCmd.AddCommand(buildCmd, serveCmd, lookupCmd)
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
