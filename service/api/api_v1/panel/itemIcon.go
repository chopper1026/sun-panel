package panel

import (
	"encoding/json"
	"fmt"
	"mime"
	"os"
	"path"
	"strings"
	"sun-panel/api/api_v1/common/apiData/commonApiStructs"
	"sun-panel/api/api_v1/common/apiData/panelApiStructs"
	"sun-panel/api/api_v1/common/apiReturn"
	"sun-panel/api/api_v1/common/base"
	"sun-panel/global"
	"sun-panel/lib/cmn"
	"sun-panel/lib/siteFavicon"
	"sun-panel/models"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
	"gorm.io/gorm"
)

type ItemIcon struct {
}

func (a *ItemIcon) Edit(c *gin.Context) {
	userInfo, _ := base.GetCurrentUserInfo(c)
	req := models.ItemIcon{}

	if err := c.ShouldBindBodyWith(&req, binding.JSON); err != nil {
		apiReturn.ErrorParamFomat(c, err.Error())
		return
	}

	if req.ItemIconGroupId == 0 {
		// apiReturn.Error(c, "Group is mandatory")
		apiReturn.ErrorParamFomat(c, "Group is mandatory")
		return
	}

	req.UserId = userInfo.ID

	// json转字符串
	if j, err := json.Marshal(req.Icon); err == nil {
		req.IconJson = string(j)
	}

	if req.ID != 0 {
		// 修改
		updateField := []string{"IconJson", "Icon", "Title", "Url", "LanUrl", "Description", "OpenMethod", "GroupId", "UserId", "ItemIconGroupId"}
		if req.Sort != 0 {
			updateField = append(updateField, "Sort")
		}
		global.Db.Model(&models.ItemIcon{}).
			Select(updateField).
			Where("id=?", req.ID).Updates(&req)
	} else {
		req.Sort = 9999
		// 创建
		global.Db.Create(&req)
	}

	apiReturn.SuccessData(c, req)
}

// 添加多个图标
func (a *ItemIcon) AddMultiple(c *gin.Context) {
	userInfo, _ := base.GetCurrentUserInfo(c)
	// type Request
	req := []models.ItemIcon{}

	if err := c.ShouldBindBodyWith(&req, binding.JSON); err != nil {
		apiReturn.ErrorParamFomat(c, err.Error())
		return
	}

	for i := 0; i < len(req); i++ {
		if req[i].ItemIconGroupId == 0 {
			apiReturn.ErrorParamFomat(c, "Group is mandatory")
			return
		}
		req[i].UserId = userInfo.ID
		// json转字符串
		if j, err := json.Marshal(req[i].Icon); err == nil {
			req[i].IconJson = string(j)
		}
	}

	global.Db.Create(&req)

	apiReturn.SuccessData(c, req)
}

// // 获取详情
// func (a *ItemIcon) GetInfo(c *gin.Context) {
// 	req := systemApiStructs.AiDrawGetInfoReq{}

// 	if err := c.ShouldBindBodyWith(&req, binding.JSON); err != nil {
// 		apiReturn.ErrorParamFomat(c, err.Error())
// 		return
// 	}

// 	userInfo, _ := base.GetCurrentUserInfo(c)

// 	aiDraw := models.AiDraw{}
// 	aiDraw.ID = req.ID
// 	if err := aiDraw.GetInfo(global.Db); err != nil {
// 		if err == gorm.ErrRecordNotFound {
// 			apiReturn.Error(c, "不存在记录")
// 			return
// 		}
// 		apiReturn.ErrorDatabase(c, err.Error())
// 		return
// 	}

// 	if userInfo.ID != aiDraw.UserID {
// 		apiReturn.ErrorNoAccess(c)
// 		return
// 	}

// 	apiReturn.SuccessData(c, aiDraw)
// }

func (a *ItemIcon) GetListByGroupId(c *gin.Context) {
	req := models.ItemIcon{}

	if err := c.ShouldBindBodyWith(&req, binding.JSON); err != nil {
		apiReturn.ErrorParamFomat(c, err.Error())
		return
	}

	userInfo, _ := base.GetCurrentUserInfo(c)
	itemIcons := []models.ItemIcon{}

	if err := global.Db.Order("sort ,created_at").Find(&itemIcons, "item_icon_group_id = ? AND user_id=?", req.ItemIconGroupId, userInfo.ID).Error; err != nil {
		apiReturn.ErrorDatabase(c, err.Error())
		return
	}

	for k, v := range itemIcons {
		json.Unmarshal([]byte(v.IconJson), &itemIcons[k].Icon)
	}

	apiReturn.SuccessListData(c, itemIcons, 0)
}

func (a *ItemIcon) Deletes(c *gin.Context) {
	req := commonApiStructs.RequestDeleteIds[uint]{}

	if err := c.ShouldBindBodyWith(&req, binding.JSON); err != nil {
		apiReturn.ErrorParamFomat(c, err.Error())
		return
	}

	userInfo, _ := base.GetCurrentUserInfo(c)
	if err := global.Db.Delete(&models.ItemIcon{}, "id in ? AND user_id=?", req.Ids, userInfo.ID).Error; err != nil {
		apiReturn.ErrorDatabase(c, err.Error())
		return
	}

	apiReturn.Success(c)
}

// 保存排序
func (a *ItemIcon) SaveSort(c *gin.Context) {
	req := panelApiStructs.ItemIconSaveSortRequest{}

	if err := c.ShouldBindBodyWith(&req, binding.JSON); err != nil {
		apiReturn.ErrorParamFomat(c, err.Error())
		return
	}

	userInfo, _ := base.GetCurrentUserInfo(c)

	transactionErr := global.Db.Transaction(func(tx *gorm.DB) error {
		// 在事务中执行一些 db 操作（从这里开始，您应该使用 'tx' 而不是 'db'）
		for _, v := range req.SortItems {
			if err := tx.Model(&models.ItemIcon{}).Where("user_id=? AND id=? AND item_icon_group_id=?", userInfo.ID, v.Id, req.ItemIconGroupId).Update("sort", v.Sort).Error; err != nil {
				// 返回任何错误都会回滚事务
				return err
			}
		}

		// 返回 nil 提交事务
		return nil
	})

	if transactionErr != nil {
		apiReturn.ErrorDatabase(c, transactionErr.Error())
		return
	}

	apiReturn.Success(c)
}

// 支持获取并直接下载对方网站图标到服务器
func (a *ItemIcon) GetSiteFavicon(c *gin.Context) {
	userInfo, _ := base.GetCurrentUserInfo(c)
	req := panelApiStructs.ItemIconGetSiteFaviconReq{}

	if err := c.ShouldBindBodyWith(&req, binding.JSON); err != nil {
		apiReturn.ErrorParamFomat(c, err.Error())
		return
	}
	resp := panelApiStructs.ItemIconGetSiteFaviconResp{}

	faviconOptions := getFaviconOptionsFromConfig()
	normalizedURL, err := siteFavicon.ValidateURLSafety(req.Url, faviconOptions)
	if err != nil {
		apiReturn.Error(c, "图标获取失败："+err.Error())
		return
	}

	iconURLs, err := siteFavicon.GetFaviconURLsWithOptions(req.Url, faviconOptions)
	if err != nil {
		apiReturn.Error(c, "图标获取失败："+err.Error())
		return
	}

	// 生成保存目录
	configUpload := global.Config.GetValueString("base", "source_path")
	savePath := fmt.Sprintf("%s/%d/%d/%d/", configUpload, time.Now().Year(), time.Now().Month(), time.Now().Day())
	isExist, _ := cmn.PathExists(savePath)
	if !isExist {
		os.MkdirAll(savePath, os.ModePerm)
	}

	// 下载
	var imgInfo *os.File
	fullUrl := ""
	var lastDownloadErr error
	for _, iconURL := range iconURLs {
		global.Logger.Debug("favicon candidate:", iconURL)
		if cache, ok := getReusableFaviconCache(iconURL); ok {
			resp.IconUrl = strings.TrimPrefix(cache.LocalSrc, ".")
			apiReturn.SuccessData(c, resp)
			return
		}
		if imgInfo, err = siteFavicon.DownloadImageWithOptions(iconURL, savePath, faviconOptions); err == nil {
			fullUrl = iconURL
			break
		}
		lastDownloadErr = err
	}
	if imgInfo == nil {
		if lastDownloadErr != nil {
			apiReturn.Error(c, "图标获取失败："+lastDownloadErr.Error())
		} else {
			apiReturn.Error(c, "图标获取失败：未找到可下载的图标")
		}
		return
	}
	global.Logger.Debug("favicon selected:", fullUrl)

	contentHash, fileSize, err := siteFavicon.HashFile(imgInfo.Name())
	if err != nil {
		_ = os.Remove(imgInfo.Name())
		apiReturn.Error(c, "图标获取失败：计算图标 hash 失败："+err.Error())
		return
	}

	if cache, ok := getReusableFaviconCacheByHash(contentHash, fileSize); ok {
		_ = os.Remove(imgInfo.Name())
		if err := saveFaviconCache(models.FaviconCache{
			SourceURL:    normalizedURL.String(),
			FinalIconURL: fullUrl,
			LocalSrc:     cache.LocalSrc,
			FileId:       cache.FileId,
			ContentHash:  cache.ContentHash,
			ContentType:  cache.ContentType,
			Size:         cache.Size,
			LastUsedAt:   time.Now(),
		}); err != nil {
			apiReturn.ErrorDatabase(c, err.Error())
			return
		}
		resp.IconUrl = strings.TrimPrefix(cache.LocalSrc, ".")
		apiReturn.SuccessData(c, resp)
		return
	}

	// 保存到数据库
	ext := path.Ext(imgInfo.Name())
	mFile := models.File{}
	fileRecord, err := mFile.AddFile(userInfo.ID, normalizedURL.Host, ext, imgInfo.Name())
	if err != nil {
		apiReturn.ErrorDatabase(c, err.Error())
		return
	}
	if err := saveFaviconCache(models.FaviconCache{
		SourceURL:    normalizedURL.String(),
		FinalIconURL: fullUrl,
		LocalSrc:     imgInfo.Name(),
		FileId:       fileRecord.ID,
		ContentHash:  contentHash,
		ContentType:  mime.TypeByExtension(ext),
		Size:         fileSize,
		LastUsedAt:   time.Now(),
	}); err != nil {
		apiReturn.ErrorDatabase(c, err.Error())
		return
	}
	resp.IconUrl = strings.TrimPrefix(imgInfo.Name(), ".")
	apiReturn.SuccessData(c, resp)
}

func getReusableFaviconCache(finalIconURL string) (models.FaviconCache, bool) {
	cache, err := (&models.FaviconCache{}).FindByFinalIconURL(global.Db, finalIconURL)
	if err != nil {
		return models.FaviconCache{}, false
	}
	if !faviconCacheFileExists(cache.LocalSrc) {
		return models.FaviconCache{}, false
	}
	_ = cache.Touch(global.Db)
	return cache, true
}

func getReusableFaviconCacheByHash(contentHash string, size int64) (models.FaviconCache, bool) {
	cache, err := (&models.FaviconCache{}).FindByContentHash(global.Db, contentHash, size)
	if err != nil {
		return models.FaviconCache{}, false
	}
	if !faviconCacheFileExists(cache.LocalSrc) {
		return models.FaviconCache{}, false
	}
	_ = cache.Touch(global.Db)
	return cache, true
}

func faviconCacheFileExists(filePath string) bool {
	if filePath == "" {
		return false
	}
	info, err := os.Stat(filePath)
	return err == nil && !info.IsDir()
}

func saveFaviconCache(cache models.FaviconCache) error {
	existing, err := (&models.FaviconCache{}).FindByFinalIconURL(global.Db, cache.FinalIconURL)
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return global.Db.Create(&cache).Error
		}
		return err
	}

	return global.Db.Model(&existing).Updates(map[string]interface{}{
		"source_url":     cache.SourceURL,
		"local_src":      cache.LocalSrc,
		"file_id":        cache.FileId,
		"content_hash":   cache.ContentHash,
		"content_type":   cache.ContentType,
		"size":           cache.Size,
		"last_used_at":   cache.LastUsedAt,
		"final_icon_url": cache.FinalIconURL,
	}).Error
}

func getFaviconOptionsFromConfig() siteFavicon.Options {
	setting, err := getEffectiveFaviconNetworkSetting()
	if err != nil {
		setting = getFaviconNetworkSettingFromConfig()
	}
	return getFaviconOptionsFromSetting(setting)
}

func splitConfigList(value string) []string {
	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			result = append(result, part)
		}
	}
	return result
}
