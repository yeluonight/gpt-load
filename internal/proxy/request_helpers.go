package proxy

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"gpt-load/internal/channel"
	app_errors "gpt-load/internal/errors"
	"gpt-load/internal/models"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
)

func (ps *ProxyServer) applyParamOverrides(bodyBytes []byte, group *models.Group) ([]byte, error) {
	if len(group.ParamOverrides) == 0 || len(bodyBytes) == 0 {
		return bodyBytes, nil
	}

	var requestData map[string]any
	if err := json.Unmarshal(bodyBytes, &requestData); err != nil {
		logrus.Warnf("failed to unmarshal request body for param override, passing through: %v", err)
		return bodyBytes, nil
	}

	for key, value := range group.ParamOverrides {
		requestData[key] = value
	}

	return json.Marshal(requestData)
}

// logUpstreamError provides a centralized way to log errors from upstream interactions.
func logUpstreamError(context string, err error) {
	if err == nil {
		return
	}
	if app_errors.IsIgnorableError(err) {
		logrus.Debugf("Ignorable upstream error in %s: %v", context, err)
	} else {
		logrus.Errorf("Upstream error in %s: %v", context, err)
	}
}

// handleGzipCompression checks for gzip encoding and decompresses the body if necessary.
func handleGzipCompression(resp *http.Response, bodyBytes []byte) []byte {
	if resp.Header.Get("Content-Encoding") == "gzip" {
		reader, gzipErr := gzip.NewReader(bytes.NewReader(bodyBytes))
		if gzipErr != nil {
			logrus.Warnf("Failed to create gzip reader for error body: %v", gzipErr)
			return bodyBytes
		}
		defer reader.Close()

		decompressedBody, readAllErr := io.ReadAll(reader)
		if readAllErr != nil {
			logrus.Warnf("Failed to decompress gzip error body: %v", readAllErr)
			return bodyBytes
		}
		return decompressedBody
	}
	return bodyBytes
}

func extractModelForRequest(req *http.Request, c *gin.Context, channelHandler channel.ChannelProxy, bodyBytes []byte) string {
	if len(bodyBytes) > 0 {
		var payload struct {
			Model string `json:"model"`
		}
		if err := json.Unmarshal(bodyBytes, &payload); err == nil && strings.TrimSpace(payload.Model) != "" {
			return strings.TrimSpace(payload.Model)
		}
	}

	if req != nil && req.URL != nil {
		parts := strings.Split(req.URL.Path, "/")
		for i, part := range parts {
			if part == "models" && i+1 < len(parts) {
				return strings.Split(parts[i+1], ":")[0]
			}
		}
	}

	if c != nil && channelHandler != nil {
		return strings.TrimSpace(channelHandler.ExtractModel(c, bodyBytes))
	}
	return ""
}

func estimateRequestTokens(bodyBytes []byte) int64 {
	if len(bodyBytes) == 0 {
		return 1
	}

	estimated := int64((len(bodyBytes) + 3) / 4)
	var payload map[string]any
	if err := json.Unmarshal(bodyBytes, &payload); err == nil {
		estimated += numericTokenField(payload, "max_tokens")
		estimated += numericTokenField(payload, "max_completion_tokens")
		estimated += numericTokenField(payload, "max_output_tokens")
	}

	if estimated < 1 {
		return 1
	}
	return estimated
}

func numericTokenField(payload map[string]any, field string) int64 {
	value, exists := payload[field]
	if !exists {
		return 0
	}

	switch v := value.(type) {
	case float64:
		if v > 0 {
			return int64(v)
		}
	case int:
		if v > 0 {
			return int64(v)
		}
	case int64:
		if v > 0 {
			return v
		}
	case json.Number:
		n, err := v.Int64()
		if err == nil && n > 0 {
			return n
		}
	case string:
		n, err := strconv.ParseInt(v, 10, 64)
		if err == nil && n > 0 {
			return n
		}
	}
	return 0
}
