package main

import (
	"context"
	"flag"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/nats-io/nats.go"

	"github.com/akhenakh/nats-provisioner/provisioner"
)

func fetchFromGit(logger *slog.Logger, repoURL, gitUser, gitPass string) (string, error) {
	tempDir, err := os.MkdirTemp("", "nats-provisioner-")
	if err != nil {
		return "", err
	}
	if err := os.Chmod(tempDir, 0700); err != nil {
		os.RemoveAll(tempDir)
		return "", fmt.Errorf("failed to secure temp directory: %w", err)
	}

	cloneOpts := &git.CloneOptions{
		URL:      repoURL,
		Progress: os.Stdout,
		Depth:    1,
	}

	if gitUser != "" && gitPass != "" {
		cloneOpts.Auth = &http.BasicAuth{Username: gitUser, Password: gitPass}
	}

	logger.Info("cloning git repository", "url", repoURL)
	if _, err = git.PlainClone(tempDir, false, cloneOpts); err != nil {
		return "", fmt.Errorf("failed to clone repository: %w", err)
	}

	return tempDir, nil
}

func main() {
	natsURL := flag.String("s", nats.DefaultURL, "NATS Server URL (or use NATS_URL env var)")
	nkey := flag.String("nkey", "", "NATS NKey Seed path (file) or use NATS_NKEY env var")
	user := flag.String("user", "", "NATS Username (or use NATS_USER env var)")
	pass := flag.String("pass", "", "NATS Password (or use NATS_PASS env var)")

	localPath := flag.String("path", "", "Local path to YAML file or directory")
	gitURL := flag.String("git-url", "", "Git repository URL containing configurations")
	gitUser := flag.String("git-user", "", "Git Username (or use GIT_USER env var)")
	gitPass := flag.String("git-pass", "", "Git Password/Token (or use GIT_PASS env var)")

	detectOrphans := flag.Bool("detect-orphans", false, "Print resources that exist on the NATS server but are not in the configuration files")
	jsonOutput := flag.Bool("json", false, "Output logs in JSON format")

	flag.Parse()

	// Use environment variables as fallback for sensitive credentials
	if *natsURL == nats.DefaultURL {
		if url := os.Getenv("NATS_URL"); url != "" {
			*natsURL = url
		}
	}
	if *nkey == "" {
		if n := os.Getenv("NATS_NKEY"); n != "" {
			*nkey = n
		}
	}
	if *user == "" {
		if u := os.Getenv("NATS_USER"); u != "" {
			*user = u
		}
	}
	if *pass == "" {
		if p := os.Getenv("NATS_PASS"); p != "" {
			*pass = p
		}
	}
	if *gitUser == "" {
		if u := os.Getenv("GIT_USER"); u != "" {
			*gitUser = u
		}
	}
	if *gitPass == "" {
		if p := os.Getenv("GIT_PASS"); p != "" {
			*gitPass = p
		}
	}

	opts := &slog.HandlerOptions{Level: slog.LevelInfo}
	var handler slog.Handler
	if *jsonOutput {
		handler = slog.NewJSONHandler(os.Stdout, opts)
	} else {
		handler = slog.NewTextHandler(os.Stdout, opts)
	}
	logger := slog.New(handler)
	slog.SetDefault(logger)

	if *localPath == "" && *gitURL == "" {
		logger.Error("you must provide either --path or --git-url")
		os.Exit(1)
	}

	targetDir := *localPath
	var cleanupDir func()

	if *gitURL != "" {
		dir, err := fetchFromGit(logger, *gitURL, *gitUser, *gitPass)
		if err != nil {
			logger.Error("git error", "error", err)
			os.Exit(1)
		}
		targetDir = dir
		cleanupDir = func() { os.RemoveAll(dir) }
		defer cleanupDir()
	}

	prov, err := provisioner.NewProvisioner(*natsURL, *nkey, *user, *pass)
	if err != nil {
		logger.Error("provisioner setup failed", "error", err)
		os.Exit(1)
	}
	defer prov.Close()

	ctx := context.Background()
	err = filepath.WalkDir(targetDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}

		ext := filepath.Ext(path)
		if ext == ".yaml" || ext == ".yml" {
			logger.Info("processing file", "path", path)
			if err := prov.ProvisionFile(ctx, path); err != nil {
				logger.Error("error processing file", "path", path, "error", err)
			}
		}
		return nil
	})

	if err != nil {
		logger.Error("failed to process configurations", "error", err)
		os.Exit(1)
	}

	logger.Info("provisioning complete")

	if *detectOrphans {
		orphans, err := prov.DetectOrphans(ctx)
		if err != nil {
			logger.Error("failed to detect orphans", "error", err)
			os.Exit(1)
		}
		if *jsonOutput {
			logger.Info("scanning for orphan resources")
			for _, o := range orphans {
				parts := strings.SplitN(o, ": ", 2)
				if len(parts) == 2 {
					logger.Warn("orphan resource detected", "kind", parts[0], "name", parts[1])
				} else {
					logger.Warn("orphan resource detected", "resource", o)
				}
			}
			logger.Info("orphan scan complete", "count", len(orphans))
		} else {
			fmt.Println("\n--- Scanning for Orphan Resources ---")
			for _, o := range orphans {
				fmt.Printf("[Orphan] %s\n", o)
			}
			if len(orphans) == 0 {
				fmt.Println("No orphan resources found. Cluster is fully in sync!")
			} else {
				fmt.Printf("Total orphan resources detected: %d\n", len(orphans))
			}
			fmt.Println("-------------------------------------")
		}
	}
}
