package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"image/png"
	"io"
	"log"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	MQTT "github.com/eclipse/paho.mqtt.golang"
	"github.com/sashabaranov/go-openai"
)

// 创建一个能力枚举
const (
	IsNotExist = -1
	// 画图
	Chat = 0
	Draw = 1
)

type MqttChatRequest struct {
	Topic       string `json:"topic"`
	Msg         string `json:"msg"`          // 文字请求
	Payload     []byte `json:"payload"`      // 语音文件的字节流, 该字段读取的是文件内容
	PayloadType string `json:"payload_type"` // 语音文件的类型,该字段代表请求的语音文件的类型
}

type MqttChatResponse struct {
	Delta       string `json:"delta"`
	Text        string `json:"Text"`
	IsFinish    bool   `json:"is_finish"`
	IsError     bool   `json:"is_error"`
	Payload     []byte `json:"payload"`      // 语音文件的字节流, 该字段读取的是文件内容
	PayloadType string `json:"payload_type"` // 语音文件的类型,该字段代表请求的语音文件的类型
}

// 全局mqtt client
var client MQTT.Client

type ChatRequest struct {
	APIKey      string   `json:"api_key"`
	Model       string   `json:"model"`
	Messages    []string `json:"messages"`
	MaxTokens   int      `json:"max_tokens"`
	Temperature float64  `json:"temperature"`
}

var HttpRroxy string

// 定义一个全局锁
var mutex sync.Mutex

var IsEnableProxy = false

// 定义一个Channel 结构是 MqttChatRequest
var ChatRequestChan = make(chan MqttChatRequest)

var ChatCache = make(map[string][]openai.ChatCompletionMessage, 10)

var gpt *openai.Client

func main() {

	url := flag.String("url", "", "support mqtt tcp url , e.g: baidu.com:1883")
	proxy := flag.String("proxy", "", "support http proxy url , e.g: http://192.168.1.1:1080")
	key := flag.String("key", "", "openai api key e.g: sk-xxxxxx")
	flag.Parse()

	// 判断参数是否全部初始化
	if *url == "" || *proxy == "" || *key == "" {
		log.Println("params dont complete , use -h to see help ")
		os.Exit(1)
	}

	MqttInit(*url)
	GPTInit(*proxy, *key)
	ListenMqttStr()
	ListenMqttAudio()
	go OpenAiAbility()
	//AudioTest()
	// 防止退出
	select {}
}

func MqttInit(url string) {
	// 创建MQTT客户端连接配置
	opts := MQTT.NewClientOptions()
	opts.AddBroker("tcp://" + url)
	opts.SetClientID("go-mqtt-chat")
	client = MQTT.NewClient(opts)
	if token := client.Connect(); token.Wait() && token.Error() != nil {
		panic(token.Error())
	}
	log.Println("MQTT 连接成功")
}

func GPTInit(proxy string, key string) {
	// gpt 初始化
	config := openai.DefaultConfig(key)
	proxyUrl, err := url.Parse(proxy)

	if err != nil {
		panic(err)
	}
	transport := &http.Transport{
		Proxy: http.ProxyURL(proxyUrl),
	}
	config.HTTPClient = &http.Client{
		Transport: transport,
	}
	gpt = openai.NewClientWithConfig(config)
}

func OpenAiAbility() {
	for req := range ChatRequestChan {
		// 将请求的消息保存到缓存中
		// 判断是否存在这样的topic
		mutex.Lock()
		if _, ok := ChatCache[req.Topic]; !ok {
			msgs := make([]openai.ChatCompletionMessage, 1)
			msgs[0] = openai.ChatCompletionMessage{
				Role:    openai.ChatMessageRoleSystem,
				Content: "你是一个聊天助手。你的名字叫做胖虎",
			}
			// 如果不存在，初始化
			ChatCache[req.Topic] = msgs
		}
		ChatCache[req.Topic] = append(ChatCache[req.Topic], openai.ChatCompletionMessage{
			Role:    openai.ChatMessageRoleUser,
			Content: req.Msg,
		})
		mutex.Unlock()
		// 调用 OpenAiAPiRequest 发起 OpenAI 请求
		go OpenAiAPiRequest(req)
	}
}

func ListenMqttStr() {

	log.Println("MQTT 文本请求监听启动")

	// 订阅MQTT的ChatRequest主题， 监听的消息格式为 MqttChatRequest
	// 将消息发送的Channel中
	client.Subscribe("ChatRequest", 0, func(client MQTT.Client, msg MQTT.Message) {
		// 将msg.Payload()转为MqttChatRequest格式
		// 将转换后的MqttChatRequest发送到ChatRequestChan中
		var req MqttChatRequest
		//log.Println("收到请求: ", string(msg.Payload()))
		json.Unmarshal(msg.Payload(), &req)
		log.Println("收到请求: ", req.Msg)
		ChatRequestChan <- req
	})

}

func OpenAiChatRequest(req MqttChatRequest) {
	chatReq := openai.ChatCompletionRequest{
		Model:     openai.GPT3Dot5Turbo,
		MaxTokens: 800,
		Messages:  ChatCache[req.Topic],
		Stream:    true,
	}
	ctx := context.Background()
	stream, err := gpt.CreateChatCompletionStream(ctx, chatReq)
	if err != nil {
		fmt.Printf("ChatCompletionStream error: %v\n", err)
		PushMqttResponse(req.Topic, MqttChatResponse{
			Delta:   "",
			Text:    "抱歉 我出错了，请再试一次",
			IsError: true,
		})
		return
	}
	defer stream.Close()
	var text string

	// 记录开始时间
	startTime := time.Now().UnixNano()

	for {
		response, err := stream.Recv()
		if errors.Is(err, io.EOF) {

			// 记录结束时间
			endTime := time.Now().UnixNano()

			// fmt.Printf("耗时：%dms 共计 %d个字 平均速率 %d 个字 / 分钟", (endTime-startTime)/1000000, len(text), len(text)/int((endTime-startTime)/1000000*60))

			// 计算平均每分钟字数
			// 1. 计算耗时
			delta := (endTime - startTime) / 1000000

			// 2. 计算字数
			text_len := utf8.RuneCountInString(text)

			// 3. 计算速率
			speed := float64(text_len) / float64(delta) * 60000

			// 打印结果
			fmt.Printf("\n对话完成! 耗时：%dms 共计 %d个字 平均速率 %.2f 个字 / 分钟", delta, text_len, speed)

			// fmt.Println("Stream finished")
			// 保存到缓存中
			mutex.Lock()
			ChatCache[req.Topic] = append(ChatCache[req.Topic], openai.ChatCompletionMessage{
				Role:    openai.ChatMessageRoleAssistant,
				Content: text,
			})
			// 判断是否超过10条缓存，如果超过则移除最早一次的数据
			// 实际判断 21是因为  10条对话时api返回的，1条 system级别提示词
			if len(ChatCache[req.Topic]) > 21 {
				ChatCache[req.Topic] = ChatCache[req.Topic][1:]
			}

			mutex.Unlock()
			PushMqttResponse(req.Topic, MqttChatResponse{
				Delta:    " ",
				Text:     " ",
				IsFinish: true,
				IsError:  false,
			})
			return
		}
		if err != nil {
			fmt.Println("Stream error: ", err)
			ReturnError(req.Topic, err)
			return
		}
		text += response.Choices[0].Delta.Content
		// 将消息发送到MQTT的ChatResponse主题中
		PushMqttResponse(req.Topic, MqttChatResponse{
			Delta:    response.Choices[0].Delta.Content,
			Text:     text,
			IsFinish: false,
			IsError:  false,
		})

		fmt.Println("streaming：", time.Now().Format(time.UnixDate), text)
	}
}

func AbilityChecker(req MqttChatRequest) int {
	// 能力检查
	// 判断 req.Msg 中是否包含能力请求的关键词
	// 如果前 5 个汉字包含 "画" 则返回 1，否则返回 0

	ability := 0
	s := req.Msg
	if len(s) >= 10 { // 至少需要 10 个字节才能包含 5 个汉字
		// 将字符串转换为 []rune 类型，以便正确处理中文字符
		runes := []rune(s)
		// 取前五个汉字
		if strings.Contains(string(runes[:5]), "画") {
			ability = 1
		}
	}

	return ability
}

func OpenAiDrawRequest(req MqttChatRequest) {

	fmt.Println("开始绘图")

	ctx := context.Background()

	reqBase64 := openai.ImageRequest{
		Prompt:         req.Msg,
		Size:           openai.CreateImageSize256x256,
		ResponseFormat: openai.CreateImageResponseFormatB64JSON,
		N:              1,
	}
	respBase64, err := gpt.CreateImage(ctx, reqBase64)
	if err != nil {
		fmt.Printf("Image creation error: %v\n", err)
		ReturnError(req.Topic, err)
		return
	}
	imgBytes, err := base64.StdEncoding.DecodeString(respBase64.Data[0].B64JSON)
	if err != nil {
		fmt.Printf("Base64 decode error: %v\n", err)
		ReturnError(req.Topic, err)

		return
	}
	r := bytes.NewReader(imgBytes)
	imgData, err := png.Decode(r)
	if err != nil {
		fmt.Printf("PNG decode error: %v\n", err)
		ReturnError(req.Topic, err)
		return
	}

	// 能够解码成功说明是一个正常的图片， 发布到MQTT
	PushMqttResponse(req.Topic, MqttChatResponse{
		Delta:       "",
		Text:        " ",
		IsError:     false,
		Payload:     imgBytes,
		PayloadType: "image/png",
	})

	file, err := os.Create("example.png")
	if err != nil {
		fmt.Printf("File creation error: %v\n", err)
		return
	}
	defer file.Close()

	if err := png.Encode(file, imgData); err != nil {
		fmt.Printf("PNG encode error: %v\n", err)
		return
	}
	fmt.Println("The image was saved as example.png")
}

func OpenAiAPiRequest(req MqttChatRequest) {

	// 能力请求
	ability := AbilityChecker(req)
	switch ability {
	case Chat:
		log.Println("请求类型：聊天")
		OpenAiChatRequest(req)
	case Draw:
		log.Println("请求类型：绘图")
		OpenAiDrawRequest(req)
	default:
		log.Println("请求类型：聊天")
		OpenAiChatRequest(req)
	}
}

func ReturnError(topic string, err error) {
	PushMqttResponse(topic, MqttChatResponse{
		Delta:   "",
		Text:    "抱歉 我出错了，请再试一次 错误信息:" + err.Error(),
		IsError: true,
	})
}

func PushMqttResponse(topic string, res MqttChatResponse) {
	//fmt.Print("发送消息：", topic, res.Text, "\n")
	// 将res转json格式，发送到mqtt的 topic主题中
	jsonStr, _ := json.Marshal(res)
	client.Publish(topic, 0, false, jsonStr)
}

// 语音请求
func AudioRequest(FilePath string) (string, error) {

	fmt.Println("开始语音请求")
	req := openai.AudioRequest{
		Model:    openai.Whisper1,
		FilePath: FilePath,
	}
	ctx := context.Background()
	resp, err := gpt.CreateTranscription(ctx, req)
	if err != nil {
		fmt.Printf("Transcription error: %v\n", err)
		return "", err
	}
	fmt.Println(resp.Text)
	return resp.Text, nil
}

// 监听语音请求，然后调用语音请求
func ListenMqttAudio() {
	log.Println("MQTT 音频请求监听启动")
	// 查看AudioTemp文件夹是否存在，不存在则创建
	_, err := os.Stat("AudioTemp")
	if err != nil {
		if os.IsNotExist(err) {
			err := os.Mkdir("AudioTemp", os.ModePerm)
			if err != nil {
				log.Println("创建Temp文件夹失败")
			}
		}
	}

	// 订阅MQTT的ChatRequest主题， 监听的消息格式为 MqttChatRequest
	// 将消息发送的Channel中
	client.Subscribe("ChatRequestAudio", 0, func(client MQTT.Client, msg MQTT.Message) {
		var req MqttChatRequest
		//	log.Println("收到音频请求: ", string(msg.Payload()))
		json.Unmarshal(msg.Payload(), &req)
		// 将req.AudioBytes 写入到AudioTemp文件夹内
		// 生成随机文件名
		rand.Seed(time.Now().UnixNano())
		randNum := rand.Intn(100000)
		FileName := fmt.Sprintf("%d.%s", randNum, req.PayloadType)
		FilePath := fmt.Sprintf("AudioTemp/%s", FileName)
		// 将req.AudioBytes 写入到AudioTemp文件夹内
		file, err := os.Create(FilePath)
		if err != nil {
			fmt.Println("创建文件失败：", err)
			ReturnError(req.Topic, err)

			return
		}
		_, err = file.Write(req.Payload)
		if err != nil {
			fmt.Println("写入文件失败：", err)
			ReturnError(req.Topic, err)

			return
		}
		file.Close()
		// 调用语音请求
		text, err := AudioRequest(FilePath)
		if err != nil {
			fmt.Println("语音请求失败：", err)
			ReturnError(req.Topic, err)
			return
		}
		req.Msg = text

		// 打印音频信息
		log.Printf("收到音频请求, 大小: %dkB, 类型 %s", len(req.Payload)/1024, req.PayloadType)

		ChatRequestChan <- req
	})
}

func AudioTest() {
	// 用于测试mqtt的音频请求
	// 读取test下的b.m4a文件
	// 延迟3秒后发送请求
	time.Sleep(3 * time.Second)
	fmt.Println("开始测试音频请求")
	file, err := os.Open("test/e.m4a")
	if err != nil {
		fmt.Println("打开文件失败：", err)
		return
	}
	// 读取文件内容
	var buf bytes.Buffer
	_, err = io.Copy(&buf, file)

	if err != nil {
		fmt.Println("读取文件失败：", err)
		return
	}

	// 将文件内容转为[]byte
	AudioBytes := buf.Bytes()
	fmt.Println("读取文件成功文件大小：", len(AudioBytes))

	// 构建请求
	req := MqttChatRequest{
		Topic:       "test",
		Msg:         "",
		Payload:     AudioBytes,
		PayloadType: "m4a",
	}

	// 将请求转为json
	jsonStr, _ := json.Marshal(req)

	// 输出大小,转化为KB
	fmt.Println("发送请求大小：", len(jsonStr)/1024, "KB")

	// 发送请求
	client.Publish("ChatRequestAudio", 0, false, jsonStr)
}
