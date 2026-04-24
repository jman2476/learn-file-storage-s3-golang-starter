package main

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/auth"
	"github.com/google/uuid"
)

func (cfg *apiConfig) handlerUploadVideo(w http.ResponseWriter, r *http.Request) {
	const uploadLimit = 1 << 30
	http.MaxBytesReader(w, r.Body, uploadLimit)

	videoIDString := r.PathValue("videoID")
	videoID, err := uuid.Parse(videoIDString)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid ID", err)
		return
	}

	token, err := auth.GetBearerToken(r.Header)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Not authorized", err)
		return
	}

	userID, err := auth.ValidateJWT(token, cfg.jwtSecret)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Unauthorized", err)
		return
	}

	metadata, err := cfg.db.GetVideo(videoID)
	if err != nil {
		respondWithError(w, http.StatusNotFound, "Not found", err)
		return
	}

	if metadata.UserID != userID {
		respondWithError(w, http.StatusUnauthorized, "Unauthorized action", err)
		return
	}

	file, header, err := r.FormFile("video")
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Unable to parse video", err)
		return
	}
	defer file.Close()

	mediaType, _, err := mime.ParseMediaType(header.Header.Get("Content-Type"))
	if mediaType != "video/mp4" || err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid file type, needs mp4", err)
		return
	}

	tempFile, err := os.CreateTemp("", "tubely-upload_temp.mp4")
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "File saving error", err)
		return
	}
	defer os.Remove("tubely-upload_temp.mp4")
	defer tempFile.Close()

	_, err = io.Copy(tempFile, file)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "File processing error", err)
		return
	}
	tempFile.Seek(0, io.SeekStart)

	filePath, err := filepath.Abs(tempFile.Name())
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Can't get file path", err)
		return
	}

	processingFilePath, err := processVideoForFastStart(filePath)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Error fast start processing", err)
		return
	}
	fastStartFile, err := os.Open(processingFilePath)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Error opening fast-start file", err)
		return
	}
	defer os.Remove(processingFilePath)
	defer fastStartFile.Close()

	aspectRatio, err := getVideoAspectRatio(filePath)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Error reading aspect ratio", err)
		return
	}

	var fileKey = make([]byte, 32)
	rand.Read(fileKey)
	videoKey := fmt.Sprintf("%s.mp4",
		base64.RawURLEncoding.EncodeToString(fileKey),
	)
	switch aspectRatio {
	case "16:9":
		videoKey = "/landscape/" + videoKey
	case "9:16":
		videoKey = "/portrait/" + videoKey
	default:
		videoKey = "/other/" + videoKey
	}

	putObjArgs := s3.PutObjectInput{
		Bucket:      &cfg.s3Bucket,
		Key:         &videoKey,
		Body:        fastStartFile,
		ContentType: &mediaType,
	}

	newVideoURL := fmt.Sprintf(
		"%s,%s",
		cfg.s3Bucket, videoKey,
	)
	metadata.VideoURL = &newVideoURL

	err = cfg.db.UpdateVideo(metadata)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Unable to update video", err)
		return
	}

	cfg.s3Client.PutObject(r.Context(), &putObjArgs)

	updatedMetadata, err := cfg.dbVideoToSignedVideo(metadata)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Unable to get URL", err)
		return
	}

	respondWithJSON(w, http.StatusOK, updatedMetadata)

}
