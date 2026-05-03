package netfox

import (
	"sort"

	"github.com/unxed/vtui"
)

type ProtocolHandler interface {
	Prefix() string
	DefaultPort() string
	BuildExtraUI(cfg *NetFoxConfig, x, y, w, h int) (vtui.UIElement, func())
}

var handlers = make(map[string]ProtocolHandler)

func RegisterProtocol(h ProtocolHandler) {
	handlers[h.Prefix()] = h
}

func GetProtocols() []string {
	var res []string
	for k := range handlers {
		res = append(res, k)
	}
	sort.Strings(res)
	return res
}
