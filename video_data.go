package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/database"
)

type ffprobe struct {
	Streams []struct {
		Height      int    `json:"height"`
		Width       int    `json:"width"`
		AspectRatio string `json:"display_aspect_ratio"`
	} `json:"streams"`
}

func getVideoAspectRatio(filePath string) (string, error) {
	ffprobeCmd := exec.Command("ffprobe", "-v", "error", "-print_format", "json", "-show_streams", filePath)

	var cmdOutput bytes.Buffer

	ffprobeCmd.Stdout = &cmdOutput
	err := ffprobeCmd.Run()
	if err != nil {
		return "", fmt.Errorf("Error running ffprobe command on %s: %w", filePath, err)
	}

	decoder := json.NewDecoder(&cmdOutput)
	videoInfo := ffprobe{}
	err = decoder.Decode(&videoInfo)
	if err != nil {
		return "", fmt.Errorf("Error getting video metadata w/ ffmprobe from video at %s: %w", filePath, err)
	}

	if videoInfo.Streams[0].AspectRatio == "" {
		switch x := float32(videoInfo.Streams[0].Width) / float32(videoInfo.Streams[0].Height); {
		case x > 1.7 && x < 1.8:
			videoInfo.Streams[0].AspectRatio = "16:9"
		case x > 0.55 && x < 0.57:
			videoInfo.Streams[0].AspectRatio = "9:16"
		default:
			videoInfo.Streams[0].AspectRatio = "other"
		}
	}

	return videoInfo.Streams[0].AspectRatio, nil

}

func processVideoForFastStart(filePath string) (string, error) {
	processingPath := filePath + ".processing"
	fastStartCmd := exec.Command("ffmpeg", "-i", filePath, "-c", "copy", "-movflags", "faststart", "-f", "mp4", processingPath)

	err := fastStartCmd.Run()
	if err != nil {
		return "", fmt.Errorf("Error processing video for fast start: %w", err)
	}

	return processingPath, nil

}

func (cfg *apiConfig) dbVideoToSignedVideo(video database.Video) (database.Video, error) {
	if video.VideoURL == nil {
		return video, nil
	}
	vidURL := *video.VideoURL
	bucket, key, ok := strings.Cut(vidURL, ",")
	if !ok {
		return database.Video{}, fmt.Errorf("Video has no URL")
	}

	presignedUrl, err := generatePresignedURL(cfg.s3Client, bucket, key, time.Minute)
	if err != nil {
		return database.Video{}, fmt.Errorf("Presigned URL error: %w", err)
	}
	video.VideoURL = &presignedUrl

	return video, nil
}
