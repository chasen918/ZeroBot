package ZeroBot

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/gorilla/websocket"
	"github.com/tidwall/gjson"
	"sync"
	"sync/atomic"
	"time"
)

type Bot struct {
	conn    *websocket.Conn
	sending chan []byte
}

var (
	zeroBot Bot
	seq     uint64 = 0
	seqMap  sync.Map
)

func Run(addr, token string) {
	zeroBot.sending = make(chan []byte)
	zeroBot.conn = connectWebsocketServer(addr, token)
	go listenEvent(zeroBot.conn, handleResponse)
	go sendChannel(zeroBot.conn, zeroBot.sending)
}

func sendAndWait(request WebSocketRequest) (APIResponse, error) {
	ch := make(chan APIResponse)
	seqMap.Store(request.Echo, ch)
	defer seqMap.Delete(request.Echo)
	data, err := json.Marshal(request)
	fmt.Println(string(data))
	if err != nil {
		return APIResponse{}, err
	}
	zeroBot.sending <- data
	select { // 等待数据返回
	case rsp, ok := <-ch:
		if !ok {
			return APIResponse{}, errors.New("channel closed")
		}
		return rsp, nil
	case <-time.After(5 * time.Second):
		return APIResponse{}, errors.New("timed out")
	}
}

func handleResponse(response []byte) {
	rsp := gjson.ParseBytes(response)
	if rsp.Get("echo").Exists() { // 存在echo字段，是api调用的返回
		if c, ok := seqMap.Load(rsp.Get("echo").Int()); ok {
			if ch, ok := c.(chan APIResponse); ok {
				defer close(ch)
				ch <- APIResponse{ // 发送api调用响应
					Status:  rsp.Get("status").Str,
					Data:    rsp.Get("data"),
					RetCode: rsp.Get("retcode").Int(),
					Echo:    rsp.Get("echo").Uint(),
				}
			}
		}
	} else {
		event := rsp.Map()
		go processEvent(event)
	}
}

func processEvent(event Event) {
	tempMatcherList.Range(func(key, value interface{}) bool {
		matcher := value.(*Matcher)
		for _, v := range matcher.Rules {
			if v(event) == false {
				return true
			}
		}
		go matcher.run(event)
		tempMatcherList.Delete(key)
		return true
	})
	for _, v := range matcherList {
		go runMatcher(v, event)
	}
}

func getSeq() uint64 {
	return atomic.AddUint64(&seq, 1)
}
