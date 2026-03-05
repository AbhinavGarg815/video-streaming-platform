package ffmpeg

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

type Transcoder struct {
	binary string
}

type OutputFile struct {
	Resolution  string
	Format      string
	Path        string
	KeySuffix   string
	ContentType string
	SizeBytes   int64
	IsManifest  bool
}

func New(binary string) *Transcoder {
	return &Transcoder{binary: binary}
}

func (t *Transcoder) Transcode(inputPath, outputDir string, resolutions []string) ([]OutputFile, error) {
	out := make([]OutputFile, 0, 32)
	variantEntries := make([]variantInfo, 0, len(resolutions))

	for _, resolution := range resolutions {
		resolution = strings.ToLower(strings.TrimSpace(resolution))
		spec, ok := resolutionSpec(resolution)
		if !ok {
			return nil, fmt.Errorf("unsupported resolution: %s", resolution)
		}

		variantDir := filepath.Join(outputDir, resolution)
		if err := os.MkdirAll(variantDir, 0o755); err != nil {
			return nil, fmt.Errorf("create variant dir: %w", err)
		}

		playlistPath := filepath.Join(variantDir, "index.m3u8")
		segmentPattern := filepath.Join(variantDir, "segment_%03d.ts")

		args := []string{
			"-y",
			"-i", inputPath,
			"-vf", fmt.Sprintf("scale=%s,format=yuv420p", spec.Scale),
			"-c:v", "libx264",
			"-preset", "fast",
			"-profile:v", "main",
			"-pix_fmt", "yuv420p",
			"-crf", "21",
			"-g", "48",
			"-keyint_min", "48",
			"-sc_threshold", "0",
			"-b:v", spec.Bitrate,
			"-maxrate", spec.MaxRate,
			"-bufsize", spec.BufSize,
			"-c:a", "aac",
			"-b:a", "128k",
			"-ar", "48000",
			"-ac", "2",
			"-hls_time", "4",
			"-hls_playlist_type", "vod",
			"-hls_segment_type", "mpegts",
			"-hls_segment_filename", segmentPattern,
			playlistPath,
		}

		cmd := exec.Command(t.binary, args...)
		output, err := cmd.CombinedOutput()
		if err != nil {
			return nil, fmt.Errorf("ffmpeg failed (%s/hls): %w - %s", resolution, err, string(output))
		}

		playlistInfo, err := os.Stat(playlistPath)
		if err != nil {
			return nil, fmt.Errorf("stat variant playlist: %w", err)
		}

		out = append(out, OutputFile{
			Resolution:  resolution,
			Format:      "hls",
			Path:        playlistPath,
			KeySuffix:   filepath.ToSlash(filepath.Join(resolution, "index.m3u8")),
			ContentType: "application/vnd.apple.mpegurl",
			SizeBytes:   playlistInfo.Size(),
			IsManifest:  true,
		})

		segmentFiles, err := filepath.Glob(filepath.Join(variantDir, "segment_*.ts"))
		if err != nil {
			return nil, fmt.Errorf("list segment files: %w", err)
		}
		sort.Strings(segmentFiles)

		for _, segmentFile := range segmentFiles {
			segmentInfo, err := os.Stat(segmentFile)
			if err != nil {
				return nil, fmt.Errorf("stat segment file: %w", err)
			}

			out = append(out, OutputFile{
				Resolution:  resolution,
				Format:      "hls-segment",
				Path:        segmentFile,
				KeySuffix:   filepath.ToSlash(filepath.Join(resolution, filepath.Base(segmentFile))),
				ContentType: "video/mp2t",
				SizeBytes:   segmentInfo.Size(),
				IsManifest:  false,
			})
		}

		variantEntries = append(variantEntries, variantInfo{
			Resolution: resolution,
			Bandwidth:  spec.Bandwidth,
			Dimensions: spec.Dimensions,
		})
	}

	masterContent := buildMasterPlaylist(variantEntries)
	masterPath := filepath.Join(outputDir, "master.m3u8")
	if err := os.WriteFile(masterPath, []byte(masterContent), 0o644); err != nil {
		return nil, fmt.Errorf("write master playlist: %w", err)
	}

	masterInfo, err := os.Stat(masterPath)
	if err != nil {
		return nil, fmt.Errorf("stat master playlist: %w", err)
	}

	out = append(out, OutputFile{
		Resolution:  "master",
		Format:      "hls",
		Path:        masterPath,
		KeySuffix:   "master.m3u8",
		ContentType: "application/vnd.apple.mpegurl",
		SizeBytes:   masterInfo.Size(),
		IsManifest:  true,
	})

	return out, nil
}

type variantInfo struct {
	Resolution string
	Bandwidth  string
	Dimensions string
}

type resolutionConfig struct {
	Scale      string
	Bitrate    string
	MaxRate    string
	BufSize    string
	Bandwidth  string
	Dimensions string
}

func resolutionSpec(resolution string) (resolutionConfig, bool) {
	switch resolution {
	case "360p":
		return resolutionConfig{Scale: "640:360", Bitrate: "900k", MaxRate: "960k", BufSize: "1800k", Bandwidth: "960000", Dimensions: "640x360"}, true
	case "480p":
		return resolutionConfig{Scale: "854:480", Bitrate: "1400k", MaxRate: "1498k", BufSize: "2800k", Bandwidth: "1498000", Dimensions: "854x480"}, true
	case "720p":
		return resolutionConfig{Scale: "1280:720", Bitrate: "2800k", MaxRate: "2996k", BufSize: "5600k", Bandwidth: "2996000", Dimensions: "1280x720"}, true
	case "1080p":
		return resolutionConfig{Scale: "1920:1080", Bitrate: "5000k", MaxRate: "5350k", BufSize: "10000k", Bandwidth: "5350000", Dimensions: "1920x1080"}, true
	default:
		return resolutionConfig{}, false
	}
}

func buildMasterPlaylist(variants []variantInfo) string {
	ordered := make([]variantInfo, len(variants))
	copy(ordered, variants)
	sort.SliceStable(ordered, func(i, j int) bool {
		return resolutionOrder(ordered[i].Resolution) < resolutionOrder(ordered[j].Resolution)
	})

	lines := []string{"#EXTM3U", "#EXT-X-VERSION:3"}
	for _, variant := range ordered {
		lines = append(lines,
			fmt.Sprintf("#EXT-X-STREAM-INF:BANDWIDTH=%s,RESOLUTION=%s,CODECS=\"avc1.4d401f,mp4a.40.2\"", variant.Bandwidth, variant.Dimensions),
			fmt.Sprintf("%s/index.m3u8", variant.Resolution),
		)
	}

	return strings.Join(lines, "\n") + "\n"
}

func resolutionOrder(resolution string) int {
	switch resolution {
	case "360p":
		return 1
	case "480p":
		return 2
	case "720p":
		return 3
	case "1080p":
		return 4
	default:
		return 99
	}
}
