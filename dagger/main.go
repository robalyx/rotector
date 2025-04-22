// Package main provides a Dagger module for building and deploying Rotector.
//
// The module is designed to be used with the Dagger CLI or SDKs to automate
// build and deployment workflows.
package main

import (
	"context"
	"dagger/rotector/internal/dagger"
	"fmt"
	"strings"
)

type Rotector struct{}

// BuildContainer creates a container image for the project.
func (m *Rotector) BuildContainer(
	ctx context.Context,
	// Source code directory
	// +required
	src *dagger.Directory,
	// Platform to build for
	// +optional
	// +default="linux/amd64"
	platform *dagger.Platform,
) (*dagger.Container, error) {
	// Use default platform if none specified
	buildPlatform := dagger.Platform("linux/amd64")
	if platform != nil {
		buildPlatform = *platform
	}

	// Get architecture using containerd utility
	platformArch, err := dag.Containerd().ArchitectureOf(ctx, buildPlatform)
	if err != nil {
		return nil, fmt.Errorf("failed to get architecture: %w", err)
	}

	// Create build container
	buildCtr := dag.Container().
		From("golang:1.24.2-alpine").
		WithMountedCache("/go/pkg/mod", dag.CacheVolume("go-mod")).
		WithMountedCache("/root/.cache/go-build", dag.CacheVolume("go-build")).
		WithDirectory("/src", src).
		WithWorkdir("/src").
		WithEnvVariable("CGO_ENABLED", "0").
		WithEnvVariable("GOOS", "linux").
		WithEnvVariable("GOARCH", platformArch)

	// Install build dependencies
	buildCtr = buildCtr.WithExec([]string{"apk", "add", "--no-cache", "upx", "ca-certificates"})

	// Create necessary directories
	buildCtr = buildCtr.
		WithExec([]string{"mkdir", "-p", "/src/bin"}).
		WithExec([]string{"mkdir", "-p", "/src/logs"})

	// Build binaries
	binaries := []string{"bot", "worker", "entrypoint"}
	for _, binary := range binaries {
		buildCtr = buildCtr.WithExec([]string{
			"go", "build",
			"-ldflags=-s -w",
			"-o", "/src/bin/" + binary,
			"./cmd/" + binary,
		})
	}

	// Compress binaries
	for _, binary := range binaries {
		buildCtr = buildCtr.WithExec([]string{"upx", "--best", "--lzma", "/src/bin/" + binary})
	}

	// Create final container
	return dag.Container(dagger.ContainerOpts{Platform: buildPlatform}).
		From("gcr.io/distroless/static-debian12:latest").
		WithDirectory("/app/bin", buildCtr.Directory("/src/bin")).
		WithDirectory("/app/logs", buildCtr.Directory("/src/logs")).
		WithFile("/etc/ssl/certs/ca-certificates.crt", buildCtr.File("/etc/ssl/certs/ca-certificates.crt")).
		WithWorkdir("/app").
		WithEntrypoint([]string{"/app/bin/entrypoint"}).
		WithEnvVariable("RUN_TYPE", "bot").
		WithEnvVariable("WORKER_TYPE", "ai").
		WithEnvVariable("WORKERS_COUNT", "1"), nil
}

// Publish the application container after building and testing it.
func (m *Rotector) Publish(
	ctx context.Context,
	// Source code directory
	// +required
	src *dagger.Directory,
	// Docker image name (e.g. "username/repo:tag")
	// +required
	imageName string,
	// Platforms to build for (comma-separated, e.g. "linux/amd64,linux/arm64")
	// +optional
	// +default="linux/amd64"
	platforms string,
) (string, error) {
	// Parse platforms string
	var platformList []dagger.Platform
	if platforms == "" {
		platformList = []dagger.Platform{"linux/amd64"}
	} else {
		for _, p := range strings.Split(platforms, ",") {
			platformList = append(platformList, dagger.Platform(strings.TrimSpace(p)))
		}
	}

	// Build containers for each platform
	platformVariants := make([]*dagger.Container, 0, len(platformList))
	for _, platform := range platformList {
		container, err := m.BuildContainer(ctx, src, &platform)
		if err != nil {
			return "", fmt.Errorf("failed to build container for %s: %w", platform, err)
		}
		platformVariants = append(platformVariants, container)
	}

	// Publish multi-arch image
	ref, err := dag.Container().Publish(ctx, imageName, dagger.ContainerPublishOpts{
		PlatformVariants: platformVariants,
	})
	if err != nil {
		return "", fmt.Errorf("failed to publish image: %w", err)
	}

	return ref, nil
}

// Run the program with specified command and config files.
func (m *Rotector) Run(
	ctx context.Context,
	// Source code directory
	// +required
	src *dagger.Directory,
	// Config directory path
	// +required
	configDir *dagger.Directory,
	// Command to run: "bot", "worker", "export" or "db"
	// +required
	cmd string,
	// Worker type for worker command (e.g. "friend", "group", "maintenance", "queue", "stats")
	// +required
	workerType *string,
	// Number of workers to run
	// +optional
	// +default="1"
	workerCount *string,
) *dagger.Container {
	// Create run container
	runCtr := dag.Container().
		From("golang:1.24.2-alpine").
		WithMountedCache("/go/pkg/mod", dag.CacheVolume("go-mod")).
		WithMountedCache("/root/.cache/go-build", dag.CacheVolume("go-build")).
		WithDirectory("/src", src).
		WithDirectory("/etc/rotector", configDir).
		WithWorkdir("/src").
		WithEnvVariable("CGO_ENABLED", "0")

	// Install dependencies
	runCtr = runCtr.WithExec([]string{"apk", "add", "--no-cache", "ca-certificates"})

	// Build the program
	runCtr = runCtr.WithExec([]string{
		"go", "build",
		"-o", "/src/bin/rotector",
		"./cmd/" + cmd,
	})

	// Set up command arguments
	args := []string{"/src/bin/rotector"}

	// Add command-specific arguments
	switch cmd {
	case "worker":
		if workerType != nil {
			args = append(args, *workerType)
		}
		if workerCount != nil {
			args = append(args, "--workers", *workerCount)
		}
	case "bot", "export", "db":
		// These commands don't need additional arguments
	}

	// Return container with command
	return runCtr.WithExec(args)
}
