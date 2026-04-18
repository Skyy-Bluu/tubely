package main

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"os/exec"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/auth"
	"github.com/google/uuid"
)

type videoInfo struct {
	Streams []streams `json:"streams"`
}

type streams struct {
	Width       int    `json:"width"`
	Height      int    `json:"height"`
	AspectRatio string `json:"display_aspect_ratio"`
}

func (cfg *apiConfig) handlerUploadVideo(w http.ResponseWriter, r *http.Request) {

	http.MaxBytesReader(w, r.Body, 1<<30)
	videoIDString := r.PathValue("videoID")

	videoID, err := uuid.Parse(videoIDString)

	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid ID", err)
		return
	}

	token, err := auth.GetBearerToken(r.Header)

	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Couldn't find JWT", err)
		return
	}

	userID, err := auth.ValidateJWT(token, cfg.jwtSecret)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Couldn't validate JWT", err)
		return
	}

	fmt.Println("uploading video for video entry", videoID, "by user", userID)

	video, err := cfg.db.GetVideo(videoID)

	if err != nil {
		respondWithError(w, http.StatusNotFound, "Unable to fetch video", err)
		return
	}

	if video.UserID != userID {
		respondWithError(w, http.StatusUnauthorized, "Unauthorised user", err)
		return
	}

	file, header, err := r.FormFile("video")

	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Unable to parse form", err)
		return
	}

	defer file.Close()

	mediaType, _, err := mime.ParseMediaType(header.Header.Get("Content-Type"))

	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Unable to determine media type", nil)
		return
	}

	if mediaType != "video/mp4" {
		respondWithError(w, http.StatusForbidden, "Invalid media type", nil)
		return
	}

	tempFile, err := os.CreateTemp("", "tubely-upload.mp4")

	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "unable to create temp file", err)
		return
	}

	defer os.Remove(tempFile.Name())
	defer tempFile.Close()

	_, err = io.Copy(tempFile, file)

	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Unable to copy file", err)
		return
	}

	_, err = tempFile.Seek(0, io.SeekStart)

	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Unable to reset file pointer before reading", err)
		return
	}

	videoAspectRatio, err := getVideoAspectRatio(tempFile.Name())

	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "unable to determine video aspect ratio", err)
		return
	}

	var aspectRatioCategorised string

	switch videoAspectRatio {
	case "16:9":
		aspectRatioCategorised = "landscape"
	case "9:16":
		aspectRatioCategorised = "portrait"
	default:
		aspectRatioCategorised = "other"

	}

	key := make([]byte, 32)

	rand.Read(key)

	encodedString := base64.RawURLEncoding.EncodeToString(key)

	fileKey := aspectRatioCategorised + "/" + encodedString + ".mp4"

	processedFilePath, err := createVideoForFastStart(tempFile.Name())

	processedFile, err := os.Open(processedFilePath)

	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "unable to read processed file", err)
	}

	cfg.s3Client.PutObject(r.Context(), &s3.PutObjectInput{
		Bucket:      &cfg.s3Bucket,
		Key:         &fileKey,
		Body:        processedFile,
		ContentType: &mediaType,
	})

	videoURL := fmt.Sprintf("https://%s.s3.%s.amazonaws.com/%s", cfg.s3Bucket, cfg.s3Region, fileKey)

	video.VideoURL = &videoURL

	if err := cfg.db.UpdateVideo(video); err != nil {
		respondWithError(w, http.StatusInternalServerError, "Unable to update video URL", err)
		return
	}

	respondWithJSON(w, http.StatusOK, video)
}

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
