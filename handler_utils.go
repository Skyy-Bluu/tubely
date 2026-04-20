package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/database"
)

func getVideoAspectRatio(filePath string) (string, error) {

	cmd := exec.Command("ffprobe", "-v", "error", "-print_format", "json", "-show_streams", filePath)

	var output bytes.Buffer

	cmd.Stdout = &output

	cmd.Run()

	var videoInfo videoInfo

	if err := json.Unmarshal(output.Bytes(), &videoInfo); err != nil {
		return "", err
	}

	aspectRatio := videoInfo.Streams[0].AspectRatio

	return aspectRatio, nil
}

func greatedCommonDivisor(a, b int) int {
	for b != 0 {
		a, b = b, a%b
	}
	return a
}

func getAspectRatio(width, height int) string {
	divisor := greatedCommonDivisor(width, height)
	return fmt.Sprintf("%d:%d", width/divisor, height/divisor)
}

func createVideoForFastStart(filepath string) (string, error) {

	outputFile := filepath + ".processing"

	cmd := exec.Command("ffmpeg", "-i", filepath, "-c", "copy", "-movflags", "faststart", "-f", "mp4", outputFile)

	if err := cmd.Run(); err != nil {
		return "", err
	}

	return outputFile, nil
}

func generatePresignedURL(ctx context.Context, s3Client *s3.Client, bucket, key string, expireTime time.Duration) (string, error) {

	presignCLient := s3.NewPresignClient(s3Client)
	presignedHTTPRequest, err := presignCLient.PresignGetObject(
		ctx,
		&s3.GetObjectInput{
			Bucket: &bucket,
			Key:    &key,
		},
		s3.WithPresignExpires(expireTime),
	)

	if err != nil {
		return "", err
	}

	return presignedHTTPRequest.URL, nil
}

func (cfg *apiConfig) dbVideoToSignedVideo(ctx context.Context, video database.Video) (database.Video, error) {
	s := strings.Split(*video.VideoURL, ",")

	bucket := s[0]
	key := s[1]

	presignedURL, err := generatePresignedURL(ctx, cfg.s3Client, bucket, key, time.Hour)

	if err != nil {
		return database.Video{}, err
	}

	video.VideoURL = &presignedURL

	return video, nil
}
