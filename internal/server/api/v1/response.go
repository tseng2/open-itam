package v1

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// Response 统一 API 响应信封
type Response struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

func Success(c *gin.Context, data interface{}) {
	c.JSON(http.StatusOK, Response{
		Code:    0,
		Message: "success",
		Data:    data,
	})
}

func Fail(c *gin.Context, httpStatus int, code int, msg string) {
	c.JSON(httpStatus, Response{
		Code:    code,
		Message: msg,
	})
}

// PageResult 分页数据载荷
type PageResult struct {
	Total int64       `json:"total"`
	Items interface{} `json:"items"`
}
