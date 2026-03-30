package api

import (
	"strconv"

	"github.com/gin-gonic/gin"
)

// parsePagination extracts limit and offset from query params with defaults and caps.
//
//	GET /v1/channels?limit=20&offset=40
//
// If limit is missing or invalid → use defaultLimit.
// If limit > maxLimit → clamp to maxLimit.
// If offset is missing or invalid → 0.
func parsePagination(c *gin.Context, defaultLimit, maxLimit int) (limit, offset int) {
	limit = defaultLimit
	if l := c.Query("limit"); l != "" {
		if v, err := strconv.Atoi(l); err == nil && v > 0 {
			limit = v
		}
	}
	if limit > maxLimit {
		limit = maxLimit
	}

	if o := c.Query("offset"); o != "" {
		if v, err := strconv.Atoi(o); err == nil && v >= 0 {
			offset = v
		}
	}
	return limit, offset
}
