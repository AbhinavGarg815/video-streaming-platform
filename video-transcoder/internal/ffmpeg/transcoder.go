package ffmpeg

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type Transcoder struct {
	binary string
}

type OutputFile struct {
	Resolution  string
	Format      string
	Path        string
	ContentType string
	SizeBytes   int64
}

func New(binary string) *Transcoder {
	return &Transcoder{binary: binary}
}

func (t *Transcoder) Transcode(inputPath, outputDir string, resolutions, formats []string) ([]OutputFile, error) {
	out := make([]OutputFile, 0, len(resolutions)*len(formats))

	for _, resolution := range resolutions {
		scale, ok := resolutionScale(resolution)
		if !ok {
			return nil, fmt.Errorf("unsupported resolution: %s", resolution)
		}

		for _, format := range formats {
			format = strings.ToLower(strings.TrimSpace(format))
			outputPath := filepath.Join(outputDir, fmt.Sprintf("%s.%s", resolution, format))

			args, contentType, err := commandArgs(inputPath, outputPath, scale, format)
			if err != nil {
				return nil, err
			}

			cmd := exec.Command(t.binary, args...)
			output, err := cmd.CombinedOutput()
			if err != nil {
				return nil, fmt.Errorf("ffmpeg failed (%s/%s): %w - %s", resolution, format, err, string(output))
			}

			info, err := os.Stat(outputPath)
			if err != nil {
				return nil, fmt.Errorf("stat transcoded file: %w", err)
			}

			out = append(out, OutputFile{
				Resolution:  resolution,
				Format:      format,
				Path:        outputPath,
				ContentType: contentType,
				SizeBytes:   info.Size(),
			})
		}
	}

	return out, nil
}

func resolutionScale(resolution string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(resolution)) {
	case "360p":
		return "640:360", true
	case "480p":
		return "854:480", true
	case "720p":
		return "1280:720", true
	case "1080p":
		return "1920:1080", true
	default:
		return "", false
	}
}

func commandArgs(inputPath, outputPath, scale, format string) ([]string, string, error) {
	common := []string{"-y", "-i", inputPath, "-vf", fmt.Sprintf("scale=%s", scale), "-c:a", "aac"}

	switch format {
	case "mp4":
		return append(common, "-c:v", "libx264", "-preset", "fast", "-movflags", "+faststart", outputPath), "video/mp4", nil
	case "webm":
		return append(common, "-c:v", "libvpx-vp9", "-b:v", "0", "-crf", "32", outputPath), "video/webm", nil
	default:
		return nil, "", fmt.Errorf("unsupported format: %s", format)
	}
}
