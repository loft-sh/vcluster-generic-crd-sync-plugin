package plugin

import (
	"github.com/loft-sh/vcluster-sdk/plugin"
	"os"
)

func GetPluginName() string {
	return os.Getenv(plugin.PLUGIN_NAME)
}
