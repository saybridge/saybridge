package main

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"
)

func publishCmd() *cobra.Command {
	var outputDir string

	cmd := &cobra.Command{
		Use:   "publish",
		Short: "Build and package plugin for marketplace",
		Long: `Validates the manifest, builds the WASM binary in release mode,
and creates a distributable .tar.gz package containing:
  - manifest.json
  - plugin.wasm

The package can be uploaded to the Saybridge Marketplace.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runPublish(".", outputDir)
		},
	}

	cmd.Flags().StringVarP(&outputDir, "output", "o", ".", "Output directory for the package")

	return cmd
}

type pluginManifest struct {
	Name        string   `json:"name"`
	Slug        string   `json:"slug"`
	Version     string   `json:"version"`
	Description string   `json:"description"`
	Author      string   `json:"author"`
	Category    string   `json:"category"`
	Hooks       []string `json:"hooks"`
	Permissions []string `json:"permissions"`
}

func runPublish(dir, outputDir string) error {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return err
	}

	fmt.Println("╔═══════════════════════════════════════════════╗")
	fmt.Println("║     📦 Saybridge Plugin Publisher             ║")
	fmt.Println("╚═══════════════════════════════════════════════╝")
	fmt.Println()

	// 1. Validate manifest
	fmt.Println("1️⃣  Validating manifest.json...")
	manifest, err := validateManifest(absDir)
	if err != nil {
		return fmt.Errorf("manifest validation failed: %w", err)
	}
	fmt.Printf("   ✅ Name: %s (v%s)\n", manifest.Name, manifest.Version)
	fmt.Printf("   ✅ Category: %s\n", manifest.Category)
	fmt.Printf("   ✅ Hooks: %v\n", manifest.Hooks)
	fmt.Println()

	// 2. Build WASM
	fmt.Println("2️⃣  Building WASM binary (release mode)...")
	wasmPath, err := BuildPlugin(absDir)
	if err != nil {
		return fmt.Errorf("build failed: %w", err)
	}
	wasmInfo, _ := os.Stat(wasmPath)
	fmt.Printf("   ✅ Built: plugin.wasm (%d bytes)\n", wasmInfo.Size())
	fmt.Println()

	// 3. Calculate checksum
	fmt.Println("3️⃣  Calculating checksum...")
	checksum, err := fileChecksum(wasmPath)
	if err != nil {
		return fmt.Errorf("checksum failed: %w", err)
	}
	fmt.Printf("   ✅ SHA256: %s\n", checksum)
	fmt.Println()

	// 4. Create package
	fmt.Println("4️⃣  Creating package...")
	packageName := fmt.Sprintf("%s-v%s.tar.gz", manifest.Slug, manifest.Version)
	packagePath := filepath.Join(outputDir, packageName)

	manifestPath := filepath.Join(absDir, "manifest.json")
	if err := createPackage(packagePath, manifestPath, wasmPath); err != nil {
		return fmt.Errorf("packaging failed: %w", err)
	}

	pkgInfo, _ := os.Stat(packagePath)
	fmt.Printf("   ✅ Package: %s (%d bytes)\n", packageName, pkgInfo.Size())
	fmt.Println()

	// Summary
	fmt.Println("═══════════════════════════════════════════════")
	fmt.Printf("📦 Package ready: %s\n", packagePath)
	fmt.Printf("   Name:     %s\n", manifest.Name)
	fmt.Printf("   Version:  %s\n", manifest.Version)
	fmt.Printf("   Size:     %d bytes\n", pkgInfo.Size())
	fmt.Printf("   Checksum: %s\n", checksum)
	fmt.Println()
	fmt.Println("To install on a Saybridge instance:")
	fmt.Printf("  1. Upload %s via Admin > Marketplace > Install\n", packageName)
	fmt.Println("  2. Or copy to the plugins/ directory and restart")

	return nil
}

func validateManifest(dir string) (*pluginManifest, error) {
	data, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		return nil, fmt.Errorf("manifest.json not found: %w", err)
	}

	var m pluginManifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}

	// Validate required fields
	if m.Name == "" {
		return nil, fmt.Errorf("missing required field: name")
	}
	if m.Slug == "" {
		return nil, fmt.Errorf("missing required field: slug")
	}
	if m.Version == "" {
		return nil, fmt.Errorf("missing required field: version")
	}
	if len(m.Hooks) == 0 {
		return nil, fmt.Errorf("missing required field: hooks (at least one hook required)")
	}

	// Validate category
	validCategories := map[string]bool{
		"utility": true, "bot": true, "integration": true,
		"automation": true, "analytics": true, "security": true,
		"productivity": true, "communication": true,
	}
	if m.Category != "" && !validCategories[m.Category] {
		return nil, fmt.Errorf("invalid category %q", m.Category)
	}

	return &m, nil
}

func fileChecksum(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}

	return fmt.Sprintf("%x", h.Sum(nil)), nil
}

func createPackage(outputPath, manifestPath, wasmPath string) error {
	out, err := os.Create(outputPath)
	if err != nil {
		return err
	}
	defer out.Close()

	gzWriter := gzip.NewWriter(out)
	defer gzWriter.Close()

	tarWriter := tar.NewWriter(gzWriter)
	defer tarWriter.Close()

	// Add manifest.json
	if err := addFileToTar(tarWriter, manifestPath, "manifest.json"); err != nil {
		return fmt.Errorf("add manifest: %w", err)
	}

	// Add plugin.wasm
	if err := addFileToTar(tarWriter, wasmPath, "plugin.wasm"); err != nil {
		return fmt.Errorf("add wasm: %w", err)
	}

	return nil
}

func addFileToTar(tw *tar.Writer, filePath, nameInTar string) error {
	f, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return err
	}

	header := &tar.Header{
		Name:    nameInTar,
		Size:    info.Size(),
		Mode:    0644,
		ModTime: time.Now(),
	}

	if err := tw.WriteHeader(header); err != nil {
		return err
	}

	_, err = io.Copy(tw, f)
	return err
}
