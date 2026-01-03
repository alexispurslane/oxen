package main

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/spf13/cobra"
	"oxen/generator"
	"oxen/server"
)

func buildSite(root string, forceRebuild bool, destDir string, siteName string) error {
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
		ForceRebuild: forceRebuild,
		SiteName:     siteName,
	}

	startTime := time.Now()
	procFiles, phase1Result := generator.GetAndProcessOrgFiles(ctx)

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

func runWatchMode(root string, forceRebuild bool, destDir string, siteName string) error {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("failed to create watcher: %w", err)
	}
	defer watcher.Close()

	// Run initial build
	fmt.Println("Starting initial build...")
	if err := buildSite(root, forceRebuild, destDir, siteName); err != nil {
		slog.Error("Initial build failed", "error", err)
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
				slog.Debug("Failed to watch directory", "path", path, "error", err)
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to walk directories: %w", err)
	}

	// Channel to signal rebuilds with file count
	rebuildChan := make(chan int, 1)

	var changedFilesCount int
	var rebuildCount int
	var debounceTimer *time.Timer

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
							slog.Debug("Failed to watch new directory", "path", event.Name, "error", err)
						}
					}
				}

				// Print watcher event for debugging
				slog.Debug("Watcher event", "op", event.Op, "path", event.Name)

				// Increment counter and debounce rebuild
				changedFilesCount++
				if debounceTimer != nil {
					debounceTimer.Stop()
				}
				debounceTimer = time.AfterFunc(100*time.Millisecond, func() {
					rebuildCount++
					rebuildChan <- changedFilesCount
					changedFilesCount = 0
					debounceTimer = nil
				})

			case err, ok := <-watcher.Errors:
				if !ok {
					return
				}
				slog.Error("Watcher error", "error", err)
			}
		}
	}()

	fmt.Printf("Watching %s for changes... (Press Ctrl+C to stop)\n", absPath)

	// Main watch loop
	for {
		numChanged := <-rebuildChan

		fmt.Printf("\nChanges detected (%d file%s changed) [Rebuild #%d], rebuilding...\n", numChanged, func() string {
			if numChanged == 1 {
				return ""
			}
			return "s"
		}(), rebuildCount)

		if err := buildSite(root, forceRebuild, destDir, siteName); err != nil {
			slog.Error("Build failed", "error", err)
		}
		fmt.Printf("\nWatching %s for changes... (Press Ctrl+C to stop)\n", absPath)
	}
}

var (
	dir      string
	force    bool
	watch    bool
	port     int
	dest     string
	siteName string
	srv      *server.Server
)

func main() {
	handler := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelError,
	})
	if os.Getenv("OXEN_DEBUG") == "true" || os.Getenv("OXEN_DEBUG") == "1" {
		handler = slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
			Level: slog.LevelDebug,
		})
	}
	slog.SetDefault(slog.New(handler))

	var rootCmd = &cobra.Command{Use: "oxen"}

	var buildCmd = &cobra.Command{
		Use:   "build <dir>",
		Short: "Build the site from <dir>",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			if watch {
				if err := runWatchMode(args[0], force, dest, siteName); err != nil {
					slog.Error("Watch mode failed", "error", err)
					os.Exit(1)
				}
			} else {
				if err := buildSite(args[0], force, dest, siteName); err != nil {
					slog.Error("Build failed", "error", err)
					os.Exit(1)
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
					slog.Error("HTTP server error", "error", err)
				}
			}()

			if watch {
				if err := runWatchMode(args[0], force, dest, siteName); err != nil {
					slog.Error("Watch mode failed", "error", err)
					os.Exit(1)
				}
			} else {
				if err := buildSite(args[0], force, dest, siteName); err != nil {
					slog.Error("Build failed", "error", err)
					os.Exit(1)
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
				slog.Error("Error getting absolute path", "error", err)
				os.Exit(1)
			}

			ctx := generator.BuildContext{
				Root: absPath,
			}
			procFiles, _ := generator.GetAndProcessOrgFiles(ctx)

			if path, found := procFiles.UuidMap.Load("id:" + args[1]); found {
				fmt.Printf("ID %s found in: %s\n", args[1], path.(string))
			} else {
				fmt.Printf("ID %s not found\n", args[1])
			}
		},
	}

	buildCmd.Flags().BoolVarP(&force, "force", "f", false, "force rebuild all files")
	buildCmd.Flags().BoolVarP(&watch, "watch", "w", false, "watch for changes and rebuild")
	buildCmd.Flags().StringVar(&dest, "dest", "public", "output directory")
	buildCmd.Flags().StringVar(&siteName, "site-name", "Neon Vagabond", "site name")

	serveCmd.Flags().BoolVarP(&force, "force", "f", false, "force rebuild all files")
	serveCmd.Flags().BoolVarP(&watch, "watch", "w", false, "watch for changes and rebuild")
	serveCmd.Flags().IntVarP(&port, "port", "p", 8080, "port to serve on")
	serveCmd.Flags().StringVar(&dest, "dest", "public", "output directory")
	serveCmd.Flags().StringVar(&siteName, "site-name", "Neon Vagabond", "site name")

	rootCmd.AddCommand(buildCmd, serveCmd, lookupCmd)
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
