package converter

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

func (p *Pipeline) convertMediaFFmpeg(
	ctx context.Context,
	inPath, srcExt, outPath, dstExt string,
	opts Options,
	report func(float64, string),
) ([]string, error) {
	ff := p.rt.Path(EngineFFmpeg)
	if ff == "" {
		return nil, errors.New("FFmpeg is required for audio/video conversion")
	}

	totalDurSec := p.probeMediaDurationSec(ctx, inPath)

	args := []string{
		"-hide_banner",
		"-loglevel", "error",
		"-nostdin",
		"-y",
		"-progress", "pipe:1",
		"-i", inPath,
	}

	bitrate := strings.TrimSpace(opts.AudioBitrate)
	if bitrate == "" {
		bitrate = "192k"
	}

	targetCat := CategoryForExt(dstExt)
	switch {
	case dstExt == "gif":
		// Two-branch Lanczos palette filter for crisp animated GIF output
		args = append(args,
			"-vf", "fps=12,scale=640:-1:flags=lanczos,split[s0][s1];[s0]palettegen[p];[s1][p]paletteuse",
			"-loop", "0",
		)

	case targetCat == CategoryAudio:
		// Strip video/cover stream when transcoding to ordinary audio formats
		args = append(args, "-vn")
		switch dstExt {
		case "mp3":
			args = append(args, "-c:a", "libmp3lame", "-b:a", bitrate)
		case "wav":
			args = append(args, "-c:a", "pcm_s16le")
		case "flac":
			args = append(args, "-c:a", "flac")
		case "m4a":
			args = append(args, "-c:a", "aac", "-b:a", bitrate, "-movflags", "+faststart")
		case "aac":
			args = append(args, "-c:a", "aac", "-b:a", bitrate)
		case "ogg":
			args = append(args, "-c:a", "libvorbis", "-b:a", bitrate)
		case "opus":
			args = append(args, "-c:a", "libopus", "-b:a", bitrate)
		case "wma":
			args = append(args, "-c:a", "wmav2", "-b:a", bitrate)
		}

	case targetCat == CategoryVideo:
		vcodec := strings.ToLower(strings.TrimSpace(opts.VideoCodec))
		if vcodec == "" {
			vcodec = "h264"
		}
		if dstExt == "webm" {
			// WebM container requires VP8/VP9/AV1 + Vorbis/Opus
			if vcodec == "av1" {
				args = append(args, "-c:v", "libsvtav1", "-crf", "32")
			} else {
				args = append(args, "-c:v", "libvpx-vp9", "-crf", "31", "-b:v", "0")
			}
			args = append(args, "-c:a", "libopus", "-b:a", bitrate)
		} else if vcodec == "copy" && srcExt == dstExt {
			args = append(args, "-c", "copy")
		} else {
			switch vcodec {
			case "h265", "hevc":
				args = append(args, "-c:v", "libx265", "-crf", "26", "-preset", "medium")
				if dstExt == "mp4" || dstExt == "mov" {
					args = append(args, "-tag:v", "hvc1")
				}
			case "av1":
				args = append(args, "-c:v", "libsvtav1", "-crf", "32")
			case "vp9":
				args = append(args, "-c:v", "libvpx-vp9", "-crf", "31", "-b:v", "0")
			case "copy":
				args = append(args, "-c:v", "copy")
			default:
				args = append(args, "-c:v", "libx264", "-crf", "22", "-preset", "medium", "-pix_fmt", "yuv420p")
			}
			args = append(args, "-c:a", "aac", "-b:a", bitrate)
			if dstExt == "mp4" || dstExt == "mov" {
				args = append(args, "-movflags", "+faststart")
			}
		}
	}

	args = append(args, outPath)

	hooks := &SubprocessHooks{
		OnStdoutLine: func(line string) {
			if report == nil || totalDurSec <= 0 {
				return
			}
			// FFmpeg -progress pipe:1 emits out_time_us=1234567 or out_time_ms=1234567 (actually microseconds in FFmpeg!)
			if val, ok := strings.CutPrefix(strings.TrimSpace(line), "out_time_us="); ok {
				if us, err := strconv.ParseFloat(val, 64); err == nil && us >= 0 {
					pct := (us / 1_000_000.0) / totalDurSec
					if pct > 0.99 {
						pct = 0.99
					}
					report(pct, fmt.Sprintf("%.1fs / %.1fs", us/1_000_000.0, totalDurSec))
				}
			}
		},
	}

	_, _, err := RunCommand(ctx, ff, args, hooks)
	return nil, err
}

var reFFmpegDuration = regexp.MustCompile(`Duration:\s*(\d+):(\d+):(\d+(?:\.\d+)?)`)

func (p *Pipeline) probeMediaDurationSec(ctx context.Context, inPath string) float64 {
	pctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	if ffprobe := p.rt.FFprobePath(); ffprobe != "" {
		stdout, _, err := RunCommand(pctx, ffprobe, []string{
			"-v", "error",
			"-show_entries", "format=duration",
			"-of", "default=noprint_wrappers=1:nokey=1",
			inPath,
		}, nil)
		if err == nil {
			if d, convErr := strconv.ParseFloat(strings.TrimSpace(stdout), 64); convErr == nil && d > 0 {
				return d
			}
		}
	}

	// Fallback: parse `ffmpeg -i inPath` stderr header
	if ff := p.rt.Path(EngineFFmpeg); ff != "" {
		_, stderr, _ := RunCommand(pctx, ff, []string{"-hide_banner", "-i", inPath}, nil)
		if m := reFFmpegDuration.FindStringSubmatch(stderr); len(m) == 4 {
			h, _ := strconv.ParseFloat(m[1], 64)
			min, _ := strconv.ParseFloat(m[2], 64)
			sec, _ := strconv.ParseFloat(m[3], 64)
			return h*3600 + min*60 + sec
		}
	}
	return 0
}
