package main

import (
	"encoding/json"
	"fmt"
	"log"
	"sync"

	MQTT "github.com/eclipse/paho.mqtt.golang"
)

type MqttChatRequest struct {
	Topic string `json:"topic"`
	Msg   string `json:"msg"`
}

type MqttChatResponse struct {
	Delta string `json:"delta"`
	Text  string `json:"Text"`
}

// 全局mqtt client
var client MQTT.Client

func main() {
	// 创建MQTT客户端连接配置
	opts := MQTT.NewClientOptions()
	opts.AddBroker("tcp://mqtt.example.com:1883") // 替换为你的MQTT代理地址
	opts.SetClientID("go-mqtt-sample")
	client = MQTT.NewClient(opts)
	if token := client.Connect(); token.Wait() && token.Error() != nil {
		panic(token.Error())
	}
}

// 定义一个全局锁
var mutex sync.Mutex

var IsEnableProxy = false

// 定义一个Channel 结构是 MqttChatRequest
var ChatRequestChan = make(chan MqttChatRequest)

// 定义一个保存最近十条聊天记录的缓存, key为主题，string为历史记录 历史记录的内容形如：
// {'role': 'system', 'content': '你是一个聊天助手。'},
// {'role': 'user', 'content': '你好！'},
// {'role': 'assistant', 'content': '你好！有什么我可以帮助你的吗？'},
// {'role': 'user', 'content': '我需要预订一张机票。'},
// {'role': 'assistant', 'content': '当天的航班还是明天的航班？'},
// {'role': 'user', 'content': '明天的航班。'},
// {'role': 'assistant', 'content': '请告诉我你的出发地和目的地。'},
// {'role': 'user', 'content': '出发地是纽约，目的地是洛杉矶。'},
// {'role': 'assistant', 'content': '好的，我会为你查询航班信息。'},
// {'role': 'assistant', 'content': '明天的航班有以下选择：...'}
var ChatCache = make(map[string]string, 10)

func Chat() {
	for {
		select {
		case req := <-ChatRequestChan:
			// 将请求的消息保存到缓存中
			// 判断是否存在这样的topic

			mutex.Lock()
			if _, ok := ChatCache[req.Topic]; !ok {
				// 如果不存在，初始化
				ChatCache[req.Topic] = "{'role': 'system', 'content': '你是一个聊天助手。你的名字叫做胖虎'}"
			}
			ChatCache[req.Topic] += fmt.Sprintf(",{'role': 'user', 'content': '%s'}", req.Msg)
			mutex.Unlock()
			// 调用 OpenAiAPiRequest 发起 OpenAI 请求
			go OpenAiAPiRequest(req)
		}
	}
}

func ListenMqtt() {
	// 订阅MQTT的ChatRequest主题， 监听的消息格式为 MqttChatRequest
	// 将消息发送的Channel中
	client.Subscribe("ChatRequest", 0, func(client MQTT.Client, msg MQTT.Message) {
		// 将msg.Payload()转为MqttChatRequest格式
		// 将转换后的MqttChatRequest发送到ChatRequestChan中
		var req MqttChatRequest
		log.Println("收到请求: ", string(msg.Payload()))
		json.Unmarshal(msg.Payload(), &req)
		ChatRequestChan <- req
	})

}

func OpenAiAPiRequest(req MqttChatRequest) {
	// openai 的chatgpt api接口请求，需要支持设置代理，通过IsEnableProxy判断是否开启

	// 将req 中的 Msg追加到ChatCache中，添加的格式: ，{'role': 'user', 'content': req.Msg}
	// 从OpenAi以流式的方式获取聊天结果
	// 获取req中的主题
	// 将从OpenAi获取的每次新增的内容都以MqttChatResponse的结构使用PushMqttResponse发送到MQTT的主题中，主题为req中的主题
	// 直到 openAi接口全部返回 ，将所有内容追加保存到ChatCache中 形同：  ,{'role': 'assistant', 'content': '明天的航班有以下选择：...'}
}

func PushMqttResponse(topic string, res MqttChatResponse) {
	// 将res转json格式，发送到mqtt的 topic主题中
}
