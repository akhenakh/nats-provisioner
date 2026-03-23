package main

import (
	"context"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/nats-io/nats.go"

	"github.com/akhenakh/nats-provisioner/provisioner"
)

func fetchFromGit(repoURL, gitUser, gitPass string) (string, error) {
	tempDir, err := os.MkdirTemp("", "nats-provisioner-")
	if err != nil {
		return "", err
	}

	cloneOpts := &git.CloneOptions{
		URL:      repoURL,
		Progress: os.Stdout,
		Depth:    1,
	}

	if gitUser != "" && gitPass != "" {
		cloneOpts.Auth = &http.BasicAuth{Username: gitUser, Password: gitPass}
	}

	log.Printf("Cloning git repository: %s...", repoURL)
	if _, err = git.PlainClone(tempDir, false, cloneOpts); err != nil {
		return "", fmt.Errorf("failed to clone repository: %w", err)
	}

	return tempDir, nil
}

func main() {
	natsURL := flag.String("s", nats.DefaultURL, "NATS Server URL")
	nkey := flag.String("nkey", "", "NATS NKey Seed")
	user := flag.String("user", "", "NATS Username")
	pass := flag.String("pass", "", "NATS Password")

	localPath := flag.String("path", "", "Local path to YAML file or directory")
	gitURL := flag.String("git-url", "", "Git repository URL containing configurations")
	gitUser := flag.String("git-user", "", "Git Username (for private repos)")
	gitPass := flag.String("git-pass", "", "Git Password/Token (for private repos)")

	detectOrphans := flag.Bool("detect-orphans", false, "Print resources that exist on the NATS server but are not in the configuration files")

	flag.Parse()

	if *localPath == "" && *gitURL == "" {
		log.Fatal("You must provide either --path or --git-url")
	}

	targetDir := *localPath
	var cleanupDir func()

	if *gitURL != "" {
		dir, err := fetchFromGit(*gitURL, *gitUser, *gitPass)
		if err != nil {
			log.Fatalf("Git error: %v", err)
		}
		targetDir = dir
		cleanupDir = func() { os.RemoveAll(dir) }
		defer cleanupDir()
	}

	prov, err := provisioner.NewProvisioner(*natsURL, *nkey, *user, *pass)
	if err != nil {
		log.Fatalf("Provisioner setup failed: %v", err)
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
			log.Printf("Processing file: %s", path)
			if err := prov.ProvisionFile(ctx, path); err != nil {
				log.Printf("Error processing %s: %v", path, err)
			}
		}
		return nil
	})

	if err != nil {
		log.Fatalf("Failed to process configurations: %v", err)
	}

	log.Println("Provisioning complete!")

	if *detectOrphans {
		fmt.Println("\n--- Scanning for Orphan Resources ---")
		orphans, err := prov.DetectOrphans(ctx)
		if err != nil {
			log.Fatalf("Failed to detect orphans: %v", err)
		}
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
