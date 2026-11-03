package middleware

import (
	"mime"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/sjson"
)

// rewriteTaskPluginJSONModel patches the top-level JSON "model" field and
// replaces BodyStorage. Non-JSON bodies are left untouched.
// 正文替换与 Key 映射共用缓存清理，后续插件读取不得沿用旧模型。
func rewriteTaskPluginJSONModel(c *gin.Context, spelling string) error {
	mediaType, _, err := mime.ParseMediaType(c.GetHeader("Content-Type"))
	if err != nil {
		return nil
	}
	if mediaType != "application/json" && !strings.HasSuffix(mediaType, "+json") {
		return nil
	}
	storage, err := common.GetBodyStorage(c)
	if err != nil {
		return err
	}
	raw, err := storage.Bytes()
	if err != nil {
		return err
	}
	patched, err := sjson.SetBytes(raw, "model", spelling)
	if err != nil {
		return err
	}
	return replaceRequestBody(c, patched)
}
