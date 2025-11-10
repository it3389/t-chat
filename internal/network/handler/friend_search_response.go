package handler

import (
	"github.com/mitchellh/mapstructure"

	"t-chat/internal/network"
)

type FriendSearchResponseHandler struct {
	pineconeService network.PineconeServiceLike
	onResponse      func(*network.FriendSearchResponse)
	logger          network.Logger
}

func NewFriendSearchResponseHandler(p network.PineconeServiceLike, onResponse func(*network.FriendSearchResponse), logger network.Logger) *FriendSearchResponseHandler {
	return &FriendSearchResponseHandler{pineconeService: p, onResponse: onResponse, logger: logger}
}

func (h *FriendSearchResponseHandler) HandleMessage(msg *network.Message) error {
	defer func() {
		if r := recover(); r != nil {
			if h.logger != nil {
				h.logger.Errorf("[FriendSearchResponseHandler] panic: %v", r)
			}
		}
	}()
	if h.logger != nil {
	
	}
	if msg.Type != network.MessageTypeFriendSearchResponse {
		return nil
	}
	respRaw, ok := msg.Metadata["friend_search_response"]
	if !ok {
		if h.logger != nil {
			h.logger.Errorf("[FriendSearchResponseHandler] Metadata 无 friend_search_response 字段")
		}
		return nil
	}

	var resp *network.FriendSearchResponse
	switch v := respRaw.(type) {
	case *network.FriendSearchResponse:
		resp = v
	case map[string]interface{}:
		var tmp network.FriendSearchResponse
		config := &mapstructure.DecoderConfig{
			Result:           &tmp,
			WeaklyTypedInput: true,
			TagName:          "json",
		}
		decoder, err := mapstructure.NewDecoder(config)
		if err != nil {
			if h.logger != nil {
				h.logger.Errorf("[FriendSearchResponseHandler] mapstructure decoder 创建失败: %v", err)
			}
			return nil
		}
		if err := decoder.Decode(v); err != nil {
			if h.logger != nil {
				h.logger.Errorf("[FriendSearchResponseHandler] mapstructure decode 失败: %v", err)
			}
			return nil
		}
		resp = &tmp
	default:
		if h.logger != nil {
			h.logger.Errorf("[FriendSearchResponseHandler] friend_search_response 字段类型错误")
		}
		return nil
	}

	if h.logger != nil {

	}
	if h.onResponse != nil {
		h.onResponse(resp)
		if h.logger != nil {
	
		}
	} else if h.logger != nil {
		h.logger.Errorf("[FriendSearchResponseHandler] onResponse 为 nil")
	}
	return nil
}

// keysOfMap 返回 map 的所有 key
func keysOfMap(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// mapToStruct 尝试把 map[string]interface{} 转为 struct
func mapToStruct(m map[string]interface{}, out interface{}) error {
	b, err := network.MarshalJSONPooled(m)
	if err != nil {
		return err
	}
	return network.UnmarshalJSONPooled(b, out)
}

func (h *FriendSearchResponseHandler) GetMessageType() string {
	return network.MessageTypeFriendSearchResponse
}

func (h *FriendSearchResponseHandler) GetPriority() int {
	return network.MessagePriorityNormal
}
