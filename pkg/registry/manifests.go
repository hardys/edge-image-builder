package registry

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/suse-edge/edge-image-builder/pkg/http"
	"github.com/suse-edge/edge-image-builder/pkg/image"
	"go.uber.org/zap"
	"gopkg.in/yaml.v3"
)

func GetAllImages(embeddedContainerImages []image.ContainerImage, manifestURLs []string, localManifestSrcDir string, manifestDownloadDest string) ([]image.ContainerImage, error) {
	var combinedManifestPaths []string
	var extractedImagesSet = make(map[string]string)

	if len(manifestURLs) != 0 {
		if manifestDownloadDest == "" {
			return nil, fmt.Errorf("manifest download destination directory not defined")
		}

		downloadedManifestPaths, err := DownloadManifests(manifestURLs, manifestDownloadDest)
		if err != nil {
			return nil, fmt.Errorf("error downloading manifests: %w", err)
		}

		combinedManifestPaths = append(combinedManifestPaths, downloadedManifestPaths...)
	}

	if localManifestSrcDir != "" {
		localManifestPaths, err := getLocalManifestPaths(localManifestSrcDir)
		if err != nil {
			return nil, fmt.Errorf("error getting local manifest paths: %w", err)
		}

		combinedManifestPaths = append(combinedManifestPaths, localManifestPaths...)
	}

	for _, manifestPath := range combinedManifestPaths {
		manifests, err := readManifest(manifestPath)
		if err != nil {
			return nil, fmt.Errorf("error reading manifest %w", err)
		}

		for _, manifestData := range manifests {
			storeManifestImageNames(manifestData, extractedImagesSet)
		}
	}

	for _, containerImage := range embeddedContainerImages {
		extractedImagesSet[containerImage.Name] = containerImage.SupplyChainKey
	}

	allImages := make([]image.ContainerImage, 0, len(extractedImagesSet))
	for imageName, supplyChainKey := range extractedImagesSet {
		containerImage := image.ContainerImage{
			Name:           imageName,
			SupplyChainKey: supplyChainKey,
		}
		allImages = append(allImages, containerImage)
	}

	return allImages, nil
}

func readManifest(manifestPath string) ([]map[string]any, error) {
	manifestFile, err := os.Open(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("error opening manifest: %w", err)
	}

	var manifests []map[string]any
	decoder := yaml.NewDecoder(manifestFile)
	for {
		var manifest map[string]any
		err = decoder.Decode(&manifest)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("error unmarshalling manifest yaml '%s': %w", manifestPath, err)
		}
		manifests = append(manifests, manifest)
	}

	if len(manifests) == 0 {
		return nil, fmt.Errorf("invalid manifest")
	}

	return manifests, nil
}

func storeManifestImageNames(data map[string]any, imageSet map[string]string) {
	var findImages func(data any)

	findImages = func(data any) {
		switch t := data.(type) {
		case map[string]any:
			for k, v := range t {
				if k == "image" {
					if imageName, ok := v.(string); ok {
						imageSet[imageName] = ""
					}
				}
				findImages(v)
			}
		case []any:
			for _, v := range t {
				findImages(v)
			}
		}
	}

	findImages(data)
}

func getLocalManifestPaths(src string) ([]string, error) {
	if src == "" {
		return nil, fmt.Errorf("manifest source directory not defined")
	}

	var manifestPaths []string

	manifests, err := os.ReadDir(src)
	if err != nil {
		return nil, fmt.Errorf("reading manifest source dir '%s': %w", src, err)
	}

	for _, manifest := range manifests {
		manifestName := strings.ToLower(manifest.Name())
		if filepath.Ext(manifestName) != ".yaml" && filepath.Ext(manifestName) != ".yml" {
			zap.S().Warnf("Skipping %s as it is not a yaml file", manifest.Name())
			continue
		}

		sourcePath := filepath.Join(src, manifest.Name())
		manifestPaths = append(manifestPaths, sourcePath)
	}

	return manifestPaths, nil
}

func DownloadManifests(manifestURLs []string, destPath string) ([]string, error) {
	var manifestPaths []string

	for index, manifestURL := range manifestURLs {
		filePath := filepath.Join(destPath, fmt.Sprintf("dl-manifest-%d.yaml", index+1))
		manifestPaths = append(manifestPaths, filePath)

		if err := http.DownloadFile(context.Background(), manifestURL, filePath, nil); err != nil {
			return nil, fmt.Errorf("downloading manifest '%s': %w", manifestURL, err)
		}
	}

	return manifestPaths, nil
}
