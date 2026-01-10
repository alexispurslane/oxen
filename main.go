package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/spf13/cobra"
	"oxen/config"
	"oxen/generator"
	"oxen/server"
)

var (
	srv *server.Server
)

const (
	defaultPort   = 8080
	defaultDest   = "public"
	watchDebounce = 100 * time.Millisecond
)

func buildSite(root string, forceRebuild bool, destDir string, cfg *config.Config) error {
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
		SiteName:     cfg.SiteName,
		BaseURL:      cfg.BaseURL,
		DefaultImage: cfg.DefaultImage,
		Author:       cfg.Author,
		LicenseName:  cfg.LicenseName,
		LicenseURL:   cfg.LicenseURL,
	}

	startTime := time.Now()

	procFiles, result := generator.NewPipeline(ctx).
		WithFullPhase(generator.FindAndProcessOrgFiles).
		WithOutputOnlyPhase(func(procFiles *generator.ProcessedFiles, ctx generator.BuildContext) generator.GenerationResult {
			pageTmpl, tagTmpl, indexTmpl, atomTmpl, _, err := generator.SetupTemplates(absPath)
			if err != nil {
				return generator.GenerationResult{Errors: 1}
			}
			return generator.GenerateHtmlPages(procFiles, ctx, pageTmpl).Add(
				generator.GenerateTagPages(procFiles, ctx, tagTmpl)).Add(
				generator.GenerateIndexPage(procFiles, ctx, indexTmpl)).Add(
				generator.GenerateAtomFeed(procFiles, ctx, atomTmpl))
		}).
		WithOutputOnlyPhase(generator.CopyStaticFiles).
		Execute()

	result.SetStartTime(startTime)
	result.PrintSummary(procFiles)

	if srv != nil {
		srv.NotifyReload()
	}

	return nil
}

func runWatchMode(ctx context.Context, root string, forceRebuild bool, destDir string, cfg *config.Config) error {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("failed to create watcher: %w", err)
	}
	defer watcher.Close()

	fmt.Println("Starting initial build...")
	if err := buildSite(root, forceRebuild, destDir, cfg); err != nil {
		slog.Error("Initial build failed", "error", err)
	}

	absPath, err := filepath.Abs(root)
	if err != nil {
		return fmt.Errorf("error getting absolute path: %w", err)
	}

	if err := watcher.Add(absPath); err != nil {
		return fmt.Errorf("failed to watch root directory: %w", err)
	}

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

	rebuildChan := make(chan int, 1)

	var changedFilesCount int
	var rebuildCount int
	var debounceTimer *time.Timer

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}

				if event.Op&(fsnotify.Create|fsnotify.Write|fsnotify.Remove|fsnotify.Rename) == 0 {
					continue
				}

				if strings.HasPrefix(event.Name, destDir) {
					continue
				}

				if event.Op&fsnotify.Create == fsnotify.Create {
					if info, err := os.Stat(event.Name); err == nil && info.IsDir() {
						if err := watcher.Add(event.Name); err != nil {
							slog.Debug("Failed to watch new directory", "path", event.Name, "error", err)
						}
					}
				}

				slog.Debug("Watcher event", "op", event.Op, "path", event.Name)

				changedFilesCount++
				if debounceTimer != nil {
					debounceTimer.Stop()
				}
				debounceTimer = time.AfterFunc(watchDebounce, func() {
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

	for {
		numChanged := <-rebuildChan

		fmt.Printf("\nChanges detected (%d file%s changed) [Rebuild #%d], rebuilding...\n", numChanged, func() string {
			if numChanged == 1 {
				return ""
			}
			return "s"
		}(), rebuildCount)

		if err := buildSite(root, forceRebuild, destDir, cfg); err != nil {
			slog.Error("Build failed", "error", err)
		}
		fmt.Printf("\nWatching %s for changes... (Press Ctrl+C to stop)\n", absPath)
	}
}

var (
	dir        string
	force      bool
	watch      bool
	port       int
	dest       string
	configJSON string
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
			cfg, err := config.LoadConfig(args[0], configJSON)
			if err != nil {
				slog.Error("Failed to load config", "error", err)
				os.Exit(1)
			}

			if watch {
				ctx := context.Background()
				if err := runWatchMode(ctx, args[0], force, dest, cfg); err != nil {
					slog.Error("Watch mode failed", "error", err)
					os.Exit(1)
				}
			} else {
				if err := buildSite(args[0], force, dest, cfg); err != nil {
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
			cfg, err := config.LoadConfig(args[0], configJSON)
			if err != nil {
				slog.Error("Failed to load config", "error", err)
				os.Exit(1)
			}

			srv = server.NewServer(dest, port)
			go func() {
				if err := srv.Run(); err != nil {
					slog.Error("HTTP server error", "error", err)
				}
			}()

			if watch {
				ctx := context.Background()
				if err := runWatchMode(ctx, args[0], force, dest, cfg); err != nil {
					slog.Error("Watch mode failed", "error", err)
					os.Exit(1)
				}
			} else {
				if err := buildSite(args[0], force, dest, cfg); err != nil {
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
			procFiles, _ := generator.FindAndProcessOrgFiles(nil, ctx)

			if path, found := procFiles.UuidMap.Load(args[1]); found {
				location := path.(generator.HeaderLocation)
				fmt.Printf("ID %s found in: %s (header index: %d)\n", args[1], location.FilePath, location.HeaderIndex)
			} else {
				fmt.Printf("ID %s not found\n", args[1])
			}
		},
	}

	buildCmd.Flags().BoolVarP(&force, "force", "f", false, "force rebuild all files")
	buildCmd.Flags().BoolVarP(&watch, "watch", "w", false, "watch for changes and rebuild")
	buildCmd.Flags().StringVar(&dest, "dest", defaultDest, "output directory")
	buildCmd.Flags().StringVar(&configJSON, "config", "", "JSON config string (overrides .oxen.json)")

	serveCmd.Flags().BoolVarP(&force, "force", "f", false, "force rebuild all files")
	serveCmd.Flags().BoolVarP(&watch, "watch", "w", false, "watch for changes and rebuild")
	serveCmd.Flags().IntVarP(&port, "port", "p", defaultPort, "port to serve on")
	serveCmd.Flags().StringVar(&dest, "dest", defaultDest, "output directory")
	serveCmd.Flags().StringVar(&configJSON, "config", "", "JSON config string (overrides .oxen.json)")

	rootCmd.AddCommand(buildCmd, serveCmd, lookupCmd)
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
