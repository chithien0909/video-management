package handler

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/labstack/echo/v4"
	ffmpeg "github.com/u2takey/ffmpeg-go"
	"gorm.io/gorm"

	"video-management/internal/entity" // Import the new entity package
)

type Handler struct {
	DB          *gorm.DB
	ProgressMu  *sync.RWMutex
	JobProgress map[string]float64
}

func (h *Handler) setProgress(id string, pct float64) {
	h.ProgressMu.Lock()
	defer h.ProgressMu.Unlock()
	if pct < 0 {
		pct = 0
	}
	if pct > 100 {
		pct = 100
	}
	h.JobProgress[id] = pct
}

func (h *Handler) getProgress(id string) float64 {
	h.ProgressMu.RLock()
	defer h.ProgressMu.RUnlock()
	return h.JobProgress[id]
}

func (h *Handler) Upload(c echo.Context) error {
	// 1. Nhận file từ form-data
	file, err := c.FormFile("file")
	if err != nil {
		c.Logger().Errorf("form file error: %v", err)
		return err
	}
	c.Logger().Infof("upload start id=%d name=%s size=%d", time.Now().Unix(), file.Filename, file.Size)

	src, err := file.Open()
	if err != nil {
		return err
	}
	defer src.Close()

	// 2. Lưu file tạm
	tmpDir := "./uploads"
	os.MkdirAll(tmpDir, 0755)

	fileID := fmt.Sprintf("%d", time.Now().Unix())
	inputPath := filepath.Join(tmpDir, fileID+"_"+file.Filename)

	dst, err := os.Create(inputPath)
	if err != nil {
		return err
	}
	defer dst.Close()

	_, err = dst.ReadFrom(src)
	if err != nil {
		c.Logger().Errorf("save file error: %v", err)
		return err
	}
	c.Logger().Infof("saved input to %s", inputPath)

	// 3. Tạo thư mục HLS output
	outputDir := filepath.Join("./hls", fileID)
	os.MkdirAll(outputDir, 0755)
	outputPath := filepath.Join(outputDir, "index.m3u8")

	// 4. Lấy tổng thời lượng video bằng ffprobe
	c.Logger().Infof("probing duration for %s", inputPath)
	probeJSON, err := ffmpeg.Probe(inputPath)
	if err != nil {
		c.Logger().Errorf("ffprobe error: %v", err)
		return fmt.Errorf("ffprobe error: %v", err)
	}
	var probe struct {
		Format struct {
			Duration string `json:"duration"`
		} `json:"format"`
	}
	if err := json.Unmarshal([]byte(probeJSON), &probe); err != nil {
		return err
	}
	totalDurationSec, _ := strconv.ParseFloat(probe.Format.Duration, 64)
	if totalDurationSec <= 0 {
		totalDurationSec = 1
	}

	c.Logger().Infof("start ffmpeg hls: input=%s output=%s", inputPath, outputPath)
	// 5. Chạy ffmpeg bằng ffmpeg-go với -progress pipe:2 và đọc từ stderr
	stream := ffmpeg.
		Input(inputPath).
		Output(
			outputPath,
			ffmpeg.KwArgs{
				"profile:v":     "baseline",
				"level":         "3.0",
				"start_number":  "0",
				"hls_time":      "10",
				"hls_list_size": "0",
				"f":             "hls",
			},
		).
		OverWriteOutput().
		GlobalArgs("-progress", "pipe:2", "-nostats")

	// Use io.Pipe with WithErrorOutput to read stderr in real-time
	r, w := io.Pipe()
	var wg sync.WaitGroup
	wg.Add(1)
	lastLoggedPct := -1.0
	go func() {
		defer wg.Done()
		scanner := bufio.NewScanner(r)
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "out_time_ms=") {
				msStr := strings.TrimPrefix(line, "out_time_ms=")
				ms, _ := strconv.ParseFloat(msStr, 64)
				curSec := ms / 1000000.0
				pct := (curSec / totalDurationSec) * 100.0
				h.setProgress(fileID, pct)
				if totalDurationSec > 0 && pct < 100 && (lastLoggedPct < 0 || pct-lastLoggedPct >= 5) {
					c.Logger().Infof("ffmpeg progress id=%s %.1f%% (%.1fs/%.1fs)", fileID, pct, curSec, totalDurationSec)
					lastLoggedPct = pct
				}
			} else if strings.HasPrefix(line, "progress=") {
				c.Logger().Infof("ffmpeg event id=%s %s", fileID, line)
			} else if strings.HasPrefix(line, "frame=") || strings.HasPrefix(line, "speed=") {
				c.Logger().Infof("ffmpeg stat id=%s %s", fileID, line)
			}
		}
	}()

	if err := stream.WithErrorOutput(w).Run(); err != nil {
		c.Logger().Errorf("ffmpeg run error: %v", err)
		w.Close()
		wg.Wait()
		return fmt.Errorf("ffmpeg error: %v", err)
	}
	w.Close()
	wg.Wait()
	c.Logger().Infof("ffmpeg finished: output=%s", outputPath)
	h.setProgress(fileID, 100)

	// 6. Trả về URL playlist
	hlsURL := "/hls/" + fileID + "/index.m3u8"

	// Save video info to database
	video := entity.Video{ // Use entity.Video
		Filename: file.Filename,
		Path:     inputPath,
		HLSPath:  hlsURL,
		FileSize: file.Size, // Populate FileSize
	}
	h.DB.Create(&video)

	return c.JSON(http.StatusOK, map[string]string{
		"message": "Upload & convert success",
		"hls_url": hlsURL,
	})
}

func (h *Handler) GetProgress(c echo.Context) error {
	id := c.Param("id")
	pct := h.getProgress(id)
	return c.JSON(http.StatusOK, map[string]interface{}{
		"id":       id,
		"progress": fmt.Sprintf("%.2f", pct),
	})
}

func (h *Handler) GetVideos(c echo.Context) error {
	var videos []entity.Video
	if err := h.DB.Find(&videos).Error; err != nil {
		c.Logger().Errorf("failed to fetch videos: %v", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to fetch videos"})
	}

	return c.JSON(http.StatusOK, videos)
}

func (h *Handler) DeleteVideo(c echo.Context) error {
	id := c.Param("id")
	videoID, err := strconv.ParseUint(id, 10, 64)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "Invalid video ID"})
	}

	var video entity.Video
	if err := h.DB.First(&video, videoID).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return c.JSON(http.StatusNotFound, map[string]string{"error": "Video not found"})
		}
		c.Logger().Errorf("failed to find video: %v", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to delete video"})
	}

	// Delete original uploaded file
	if err := os.Remove(video.Path); err != nil {
		c.Logger().Warnf("failed to delete original video file %s: %v", video.Path, err)
	} else {
		c.Logger().Infof("deleted original video file: %s", video.Path)
	}

	// Delete HLS directory
	hlsDir := filepath.Dir(video.HLSPath) // Assuming HLSPath is like /hls/fileID/index.m3u8
	// Extract fileID from HLSPath
	parts := strings.Split(hlsDir, "/")
	if len(parts) > 0 {
		fileID := parts[len(parts)-1]
		fullHlsDirPath := filepath.Join("./hls", fileID)
		if err := os.RemoveAll(fullHlsDirPath); err != nil {
			c.Logger().Warnf("failed to delete HLS directory %s: %v", fullHlsDirPath, err)
		} else {
			c.Logger().Infof("deleted HLS directory: %s", fullHlsDirPath)
		}
	}


	if err := h.DB.Delete(&video).Error; err != nil {
		c.Logger().Errorf("failed to delete video from DB: %v", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to delete video"})
	}

	return c.JSON(http.StatusOK, map[string]string{"message": "Video deleted successfully"})
}
