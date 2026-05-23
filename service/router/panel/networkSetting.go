package panel

import (
	"sun-panel/api/api_v1"
	"sun-panel/api/api_v1/middleware"

	"github.com/gin-gonic/gin"
)

func InitNetworkSetting(router *gin.RouterGroup) {
	api := api_v1.ApiGroupApp.ApiPanel.NetworkSetting
	r := router.Group("", middleware.LoginInterceptor, middleware.AdminInterceptor)
	{
		r.POST("/panel/networkSetting/getFavicon", api.GetFavicon)
		r.POST("/panel/networkSetting/setFavicon", api.SetFavicon)
		r.POST("/panel/networkSetting/testFavicon", api.TestFavicon)
	}
}
