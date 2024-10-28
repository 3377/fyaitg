package main

import (
    "bytes"
    "encoding/json"
    "fmt"
    "io/ioutil"
    "log"
    "net/http"
    "strings"
    "sync"
    "time"
    "regexp"

    tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
    "gopkg.in/yaml.v2"
)

// Config结构体定义
type Config struct {
    TelegramToken         string       `yaml:"telegram_token"`
    OpenAIConfig          OpenAIConfig `yaml:"openai_config"`
    DefaultModel          string       `yaml:"default_model"`
    SystemPrompt          string       `yaml:"system_prompt"`
    HistoryLength         int          `yaml:"history_length"`
    HistoryTimeoutMinutes int          `yaml:"history_timeout_minutes"`
    AllowedUsers          []int64      `yaml:"allowed_users"`
    AllowedChannels       []string     `yaml:"allowed_channels"`
}

type OpenAIConfig struct {
    APIKey string `yaml:"api_key"`
    APIURL string `yaml:"api_url"`
}

type OpenAIModel struct {
    ID      string `json:"id"`
    Object  string `json:"object"`
    Created int    `json:"created"`
    OwnedBy string `json:"owned_by"`
}

type OpenAIModelResponse struct {
    Data []OpenAIModel `json:"data"`
}

type OpenAIRequest struct {
    Model    string    `json:"model"`
    Messages []Message `json:"messages"`
}

type Message struct {
    Role    string    `json:"role"`
    Content string    `json:"content"`
    Time    time.Time `json:"time"`
}

type OpenAIResponse struct {
    Choices []struct {
        Message struct {
            Content string `json:"content"`
        } `json:"message"`
    } `json:"choices"`
    Usage *struct {
        PromptTokens     int `json:"prompt_tokens"`
        CompletionTokens int `json:"completion_tokens"`
        TotalTokens      int `json:"total_tokens"`
    } `json:"usage"`
}

type OpenAIErrorResponse struct {
    Error struct {
        Code    string `json:"code"`
        Message string `json:"message"`
        Type    string `json:"type"`
    } `json:"error"`
}

// 全局变量
var (
    config                  Config
    currentModel            string
    conversationHistory     []Message
    availableModels         []OpenAIModel
    version                 string
    systemPrompt            string
    remainingRounds         int
    startTime               time.Time
    interactionTime         time.Time
    totalInputTokens        int
    totalOutputTokens       int
)

const (
    maxRetries = 3
    retryDelay = 5 * time.Second
)

func main() {
    log.SetFlags(0)
    log.SetOutput(new(HumanReadableLogger))

    loadConfig()
    loadVersion()

    systemPrompt = config.SystemPrompt
    remainingRounds = config.HistoryLength
    startTime = time.Now()
    interactionTime = startTime

    logEvent("ConfigLoaded", map[string]interface{}{
        "systemPrompt": systemPrompt,
    })

    availableModels = getOpenAIModels()
    if config.DefaultModel != "" {
        currentModel = config.DefaultModel
    } else if len(availableModels) > 0 {
        currentModel = availableModels[0].ID
    }

    bot, err := tgbotapi.NewBotAPI(config.TelegramToken)
    if err != nil {
        logEvent("BotInitError", err)
        log.Fatalf("Failed to initialize bot. Exiting...")
    }

    bot.Debug = true
    logEvent("BotAuthorized", map[string]interface{}{
        "username": bot.Self.UserName,
        "version":  version,
        "model":    currentModel,
        "apiURL":   config.OpenAIConfig.APIURL,
    })
    //初始化机器人菜单
setCommands(bot)

    if systemPrompt != "" {
        conversationHistory = append(conversationHistory, Message{Role: "system", Content: systemPrompt, Time: time.Now()})
    }

    for _, userID := range config.AllowedUsers {
        sendInitInfo(bot, userID)
    }

    u := tgbotapi.NewUpdate(0)
    u.Timeout = 60
    updates := bot.GetUpdatesChan(u)

    for update := range updates {
        if update.CallbackQuery != nil {
            handleCallbackQuery(bot, update.CallbackQuery)
            continue
        }
        if update.Message == nil {
            continue
        }
        if !isAllowed(update.Message.Chat.ID, update.Message.Chat.UserName) {
            continue
        }
        if update.Message.IsCommand() {
            handleCommand(bot, update.Message)
        } else {
            go handleMessage(bot, update.Message)
        }
    }
}
func setCommands(bot *tgbotapi.BotAPI) {
    commands := []tgbotapi.BotCommand{
        {
            Command:     "start",
            Description: "开始使用机器人",
        },
        {
            Command:     "models",
            Description: "查看可用的模型列表",
        },
        {
            Command:     "clear",
            Description: "清除对话历史",
        },
    }

    cmd := tgbotapi.NewSetMyCommands(commands...)
    _, err := bot.Request(cmd)
    if err != nil {
        logEvent("SetCommandsError", err)
    } else {
        logEvent("CommandsSet", map[string]interface{}{
            "commands": commands,
        })
    }
}
func loadConfig() {
    configFile, err := ioutil.ReadFile("/app/config/config.yaml")
    if err != nil {
        log.Fatal(err)
    }
    err = yaml.Unmarshal(configFile, &config)
    if err != nil {
        log.Fatal(err)
    }
}

func loadVersion() {
    versionFile, err := ioutil.ReadFile("/app/version")
    if err != nil {
        log.Fatal(err)
    }
    version = strings.TrimSpace(string(versionFile))
}

func isAllowed(chatID int64, chatUsername string) bool {
    if len(config.AllowedUsers) == 0 && len(config.AllowedChannels) == 0 {
        return true
    }
    for _, id := range config.AllowedUsers {
        if id == chatID {
            return true
        }
    }
    for _, channel := range config.AllowedChannels {
        if channel == chatUsername {
            return true
        }
    }
    return false
}

func handleCommand(bot *tgbotapi.BotAPI, message *tgbotapi.Message) {
    switch message.Command() {
    case "start":
        sendInitInfo(bot, message.Chat.ID)
    case "models":
        sendModelList(bot, message.Chat.ID)
    case "clear":
        clearConversationHistory(bot, message.Chat.ID)
    }
}

func handleMessage(bot *tgbotapi.BotAPI, message *tgbotapi.Message) {
    logEvent("ReceivedMessage", map[string]interface{}{
        "text": message.Text,
    })
    start := time.Now()

    now := time.Now()

    var newHistory []Message
    cutoffTime := now.Add(-time.Duration(config.HistoryTimeoutMinutes) * time.Minute)
    for _, msg := range conversationHistory {
        if msg.Time.After(cutoffTime) {
            newHistory = append(newHistory, msg)
        }
    }
    conversationHistory = newHistory

    conversationHistory = append(conversationHistory, Message{Role: "user", Content: message.Text, Time: now})

    if remainingRounds > 0 {
        remainingRounds--
    } else {
        remainingRounds = config.HistoryLength
        conversationHistory = conversationHistory[:0]
        if systemPrompt != "" {
            conversationHistory = append(conversationHistory, Message{Role: "system", Content: systemPrompt, Time: now})
        }
    }

    var response string
    var inputTokens, outputTokens int
    var isAPITokenCount bool
    var err error

    wg := sync.WaitGroup{}
    wg.Add(1)

    go func() {
        defer wg.Done()
        response, inputTokens, outputTokens, isAPITokenCount, err = callOpenAIWithRetry(conversationHistory)
    }()

    wg.Wait()
    duration := time.Since(start)

    if time.Since(interactionTime).Minutes() >= float64(config.HistoryTimeoutMinutes) {
        interactionTime = time.Now()
    }

    remainingTime := config.HistoryTimeoutMinutes*60 - int(time.Since(interactionTime).Seconds())

    remainingMinutes := remainingTime / 60
    remainingSeconds := remainingTime % 60

    totalInputTokens += inputTokens
    totalOutputTokens += outputTokens

    var formattedResponse string
    if err != nil {
        formattedResponse = fmt.Sprintf("抱歉，发生了错误：%s\n请检查日志以获取更多信息。", escapeMarkdownV2(err.Error()))
    } else {
        formattedResponse = formatResponse(response, inputTokens, outputTokens, isAPITokenCount, duration, remainingRounds, remainingMinutes, remainingSeconds)
    }

    msg := tgbotapi.NewMessage(message.Chat.ID, formattedResponse)
    msg.ParseMode = "MarkdownV2"
    logEvent("SendingMessage", map[string]interface{}{
        "text": formattedResponse,
    })
    sentMsg, err := bot.Send(msg)
    if err != nil {
        logEvent("SendMessageError", err)
        plainMsg := tgbotapi.NewMessage(message.Chat.ID, "抱歉，在发送格式化消息时遇到了问题。这是未格式化的回复：\n\n"+response)
        plainMsg.ParseMode = ""
        sentMsg, err = bot.Send(plainMsg)
        if err != nil {
            logEvent("SendPlainMessageError", err)
        } else {
            logSentMessage(sentMsg)
        }
    } else {
        logSentMessage(sentMsg)
    }
}

func sendInitInfo(bot *tgbotapi.BotAPI, chatID int64) {
    initInfo := fmt.Sprintf(
        "🤖 机器人初始化信息 🤖\n"+
            "──────────────\n"+
            "📅  启动时间: %s\n"+
            "🔢  系统版本: %s\n"+
            "⚙️  当前模型: %s\n"+
            "🌐  API地址: %s\n"+
            "🔄  轮数限制: %d\n"+
            "⏲️  记忆保留: %d 分钟\n"+
            "──────────────",
        startTime.Format("2006-01-02 15:04:05"), version, currentModel, config.OpenAIConfig.APIURL, config.HistoryLength, config.HistoryTimeoutMinutes)
    msg := tgbotapi.NewMessage(chatID, escapeMarkdownV2(initInfo))
    msg.ParseMode = "MarkdownV2"
    bot.Send(msg)
}

func sendModelList(bot *tgbotapi.BotAPI, chatID int64) {
    logEvent("SendingModelList", map[string]interface{}{
        "chatID": chatID,
    })

    availableModels = getOpenAIModels()
    var keyboard [][]tgbotapi.InlineKeyboardButton
    for i := 0; i < len(availableModels); i += 2 {
        row := []tgbotapi.InlineKeyboardButton{
            tgbotapi.NewInlineKeyboardButtonData(availableModels[i].ID, "model:"+availableModels[i].ID),
        }
        if i+1 < len(availableModels) {
            row = append(row, tgbotapi.NewInlineKeyboardButtonData(availableModels[i+1].ID, "model:"+availableModels[i+1].ID))
        }
        keyboard = append(keyboard, row)
    }

    msg := tgbotapi.NewMessage(chatID, "请选择一个模型:")
    msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(keyboard...)

    sentMsg, err := bot.Send(msg)
    if err != nil {
        logEvent("SendModelListError", err)
    } else {
        logEvent("ModelListSent", map[string]interface{}{
            "message": sentMsg,
        })
    }
}

func clearConversationHistory(bot *tgbotapi.BotAPI, chatID int64) {
    conversationHistory = []Message{}
    remainingRounds = config.HistoryLength
    interactionTime = time.Now()
    if systemPrompt != "" {
        conversationHistory = append(conversationHistory, Message{Role: "system", Content: systemPrompt, Time: interactionTime})
    }
    msg := tgbotapi.NewMessage(chatID, "对话记忆已清除")
    bot.Send(msg)
}

func getOpenAIModels() []OpenAIModel {
    client := &http.Client{}
    req, err := http.NewRequest("GET", config.OpenAIConfig.APIURL+"/models", nil)
    if err != nil {
        logEvent("GetModelsRequestError", err)
        return nil
    }
    req.Header.Add("Authorization", "Bearer "+config.OpenAIConfig.APIKey)

    resp, err := client.Do(req)
    if err != nil {
        logEvent("GetModelsResponseError", err)
        return nil
    }
    defer resp.Body.Close()

    body, err := ioutil.ReadAll(resp.Body)
    if err != nil {
        logEvent("ReadModelsBodyError", err)
        return nil
    }

    var modelResp OpenAIModelResponse
    err = json.Unmarshal(body, &modelResp)
    if err != nil {
        logEvent("UnmarshalModelsError", err)
        return nil
    }

    return modelResp.Data
}

func callOpenAIWithRetry(history []Message) (string, int, int, bool, error) {
    var lastErr error
    for i := 0; i < maxRetries; i++ {
        response, inputTokens, outputTokens, isAPITokenCount, err := callOpenAI(history)
        if err == nil {
            return response, inputTokens, outputTokens, isAPITokenCount, nil
        }
        lastErr = err
        logEvent("OpenAIRetry", map[string]interface{}{
            "attempt":    i + 1,
            "error":      err,
            "retryDelay": retryDelay,
        })
        time.Sleep(retryDelay)
    }
    return "", 0, 0, false, fmt.Errorf("All attempts failed. Last error: %v", lastErr)
}

func callOpenAI(history []Message) (string, int, int, bool, error) {
    logEvent("OpenAIRequest", map[string]interface{}{
        "history": history,
    })

    requestBody := OpenAIRequest{
        Model:    currentModel,
        Messages: history,
    }

    jsonBody, err := json.Marshal(requestBody)
    if err != nil {
        logEvent("MarshalRequestError", err)
        return "", 0, 0, false, fmt.Errorf("Error processing request")
    }

    req, err := http.NewRequest("POST", config.OpenAIConfig.APIURL+"/chat/completions", bytes.NewBuffer(jsonBody))
    if err != nil {
        logEvent("CreateRequestError", err)
        return "", 0, 0, false, fmt.Errorf("Error processing request")
    }

    req.Header.Set("Content-Type", "application/json")
    req.Header.Set("Authorization", "Bearer "+config.OpenAIConfig.APIKey)

    client := &http.Client{
        Timeout: 60 * time.Second,
    }
    resp, err := client.Do(req)
    if err != nil {
        logEvent("SendRequestError", err)
        return "", 0, 0, false, fmt.Errorf("Error processing request")
    }
    defer resp.Body.Close()

    body, err := ioutil.ReadAll(resp.Body)
    if err != nil {
        logEvent("ReadResponseBodyError", err)
        return "", 0, 0, false, fmt.Errorf("Error processing response")
    }

    var openAIResp OpenAIResponse
    err = json.Unmarshal(body, &openAIResp)
    if err != nil {
        var errorResp OpenAIErrorResponse
        if json.Unmarshal(body, &errorResp) == nil {
            return "", 0, 0, false, fmt.Errorf("API Error: %s - %s", errorResp.Error.Type, errorResp.Error.Message)
        }
        logEvent("UnmarshalResponseError", err)
        return "", 0, 0, false, fmt.Errorf("Error processing response")
    }

    if len(openAIResp.Choices) > 0 {
        response := openAIResp.Choices[0].Message.Content
        conversationHistory = append(conversationHistory, Message{Role: "assistant", Content: response, Time: time.Now()})

        var inputTokens, outputTokens int
        var isAPITokenCount bool

        if openAIResp.Usage != nil && openAIResp.Usage.PromptTokens > 0 && openAIResp.Usage.CompletionTokens > 0 {
            inputTokens = openAIResp.Usage.PromptTokens
            outputTokens = openAIResp.Usage.CompletionTokens
            isAPITokenCount = true
        } else {
            inputTokens = calculateTokens(history)
            outputTokens = calculateTokens(response)
            isAPITokenCount = false
        }

        return response, inputTokens, outputTokens, isAPITokenCount, nil
    }

    logEvent("NoChoicesInResponseError", nil)
    return "", 0, 0, false, fmt.Errorf("No response from AI")
}

func calculateTokens(history interface{}) int {
    text := ""
    switch v := history.(type) {
    case string:
        text = v
    case []Message:
        for _, msg := range v {
            text += msg.Content + " "
        }
    }
    words := strings.Fields(text)
    tokens := 0
    for _, word := range words {
        tokens += (len(word) + 3) / 4
    }
    return tokens
}

func handleCallbackQuery(bot *tgbotapi.BotAPI, query *tgbotapi.CallbackQuery) {
    logEvent("HandleCallbackQuery", map[string]interface{}{
        "query": query,
    })

    if !strings.HasPrefix(query.Data, "model:") {
        logEvent("UnexpectedCallbackData", map[string]interface{}{
            "data": query.Data,
        })
        return
    }

    newModel := strings.TrimPrefix(query.Data, "model:")
    logEvent("ModelChangeRequested", map[string]interface{}{
        "model": newModel,
    })

    currentModel = newModel

    confirmMsg := tgbotapi.NewMessage(query.Message.Chat.ID, fmt.Sprintf("模型已更新为：%s", currentModel))
    sentMsg, err := bot.Send(confirmMsg)
    if err != nil {
        logEvent("SendConfirmMessageError", err)
    } else {
        logEvent("ConfirmMessageSent", map[string]interface{}{
            "message": sentMsg,
        })
    }

    deleteMsg := tgbotapi.NewDeleteMessage(query.Message.Chat.ID, query.Message.MessageID)
    resp, err := bot.Request(deleteMsg)
    if err != nil {
        logEvent("DeleteModelSelectionMessageError", err)
    } else {
        logEvent("ModelSelectionMessageDeleted", map[string]interface{}{
            "response": resp,
        })
    }

    callback := tgbotapi.NewCallback(query.ID, fmt.Sprintf("模型已更新为 %s", currentModel))
    resp, err = bot.Request(callback)
    if err != nil {
        logEvent("AnswerCallbackQueryError", err)
    } else {
        logEvent("CallbackQueryAnswered", map[string]interface{}{
            "response": resp,
        })
    }

    sendInitInfo(bot, query.Message.Chat.ID)
}

func formatResponse(response string, inputTokens, outputTokens int, isAPITokenCount bool, duration time.Duration, remainingRounds, remainingMinutes, remainingSeconds int) string {
    // 添加模型信息到顶部，确保特殊字符被正确转义
    modelInfo := fmt.Sprintf("🤖 \\*%s\\*\n", escapeMarkdownV2(currentModel))
    
    // 处理主要响应内容，确保所有特殊字符被正确转义
    escapedResponse := escapeMarkdownV2(response)
    formattedResponse := modelInfo + escapedResponse

    tokenSource := "API值"
    if !isAPITokenCount {
        tokenSource = "估算"
    }

    // 统计信息部分
    stats := fmt.Sprintf("\n\n━━━━━━ 统计信息 ━━━━━━\n"+
        "📊 输入: %d \\(%s\\)    总输入: %d\n"+
        "📈 输出: %d \\(%s\\)    总输出: %d\n"+
        "⏱ 处理时间: %.2f秒\n"+
        "🔄 剩余对话轮数: %d\n"+
        "🕒 剩余有效时间: %d分钟 %d秒\n"+
        "🤖 当前使用模型: %s\n"+
        "━━━━━━━━━━━━━━━━━",
        inputTokens, escapeMarkdownV2(tokenSource), totalInputTokens,
        outputTokens, escapeMarkdownV2(tokenSource), totalOutputTokens,
        duration.Seconds(), remainingRounds,
        remainingMinutes, remainingSeconds,
        escapeMarkdownV2(currentModel))

    return formattedResponse + stats
}

func escapeMarkdownV2(text string) string {
    // 定义需要转义的特殊字符
    specialChars := []string{
        "_", "*", "[", "]", "(", ")", "~", "`", ">", 
        "#", "+", "-", "=", "|", "{", "}", ".", "!", 
        ",", ":", ";", "/", "\\", "^", "$", "&", "%",
        "<", "'"
    }
    
    // 第一步：转义所有特殊字符
    for _, char := range specialChars {
        text = strings.ReplaceAll(text, char, "\\"+char)
    }
    
    // 第二步：恢复已经正确转义的字符
    for _, char := range specialChars {
        text = strings.ReplaceAll(text, "\\\\"+char, "\\"+char)
    }
    
    return text
}

// 移除所有 Markdown 格式标记的函数，用于降级显示
func stripMarkdown(text string) string {
    // 移除所有 Markdown 语法标记
    markdownSyntax := []string{
        "*", "_", "`", "~", ">", "#", "+", "-", "=", "|",
        "[", "]", "(", ")", "{", "}", "\\",
    }
    
    for _, syntax := range markdownSyntax {
        text = strings.ReplaceAll(text, syntax, "")
    }
    
    return text
}

func sendMessage(bot *tgbotapi.BotAPI, chatID int64, text string) {
    formattedText := escapeMarkdownV2(text)
    msg := tgbotapi.NewMessage(chatID, formattedText)
    msg.ParseMode = "Markdown"

    if _, err := bot.Send(msg); err != nil {
        log.Printf("Error sending message: %v", err)
        fallbackMsg := tgbotapi.NewMessage(chatID, "抱歉，在发送格式化消息时遇到了问题。这是未格式化的回复：\n\n"+text)
        fallbackMsg.ParseMode = ""
        bot.Send(fallbackMsg)
    }
}

func logSentMessage(msg tgbotapi.Message) {
    logEvent("MessageSent", map[string]interface{}{
        "message": msg,
    })
}

func logEvent(event string, details interface{}) {
    logEntry := map[string]interface{}{
        "event":     event,
        "timestamp": time.Now().Format(time.RFC3339),
        "details":   details,
    }
    jsonLog, err := json.MarshalIndent(logEntry, "", "  ")
    if err != nil {
        fmt.Printf("Error marshalling log entry: %v\n", err)
    } else {
        fmt.Println(string(jsonLog))
    }
}

type HumanReadableLogger struct{}

func (l *HumanReadableLogger) Write(p []byte) (n int, err error) {
    var logEntry map[string]interface{}
    err = json.Unmarshal(p, &logEntry)
    if err != nil {
        fmt.Println(string(p))
        return len(p), nil
    }

    logEntry["timestamp"] = time.Now().Format(time.RFC3339)

    if response, ok := logEntry["response"].(string); ok {
        var responseJSON map[string]interface{}
        err = json.Unmarshal([]byte(response), &responseJSON)
        if err == nil {
            logEntry["response"] = responseJSON
        }
    }

    jsonLog, err := json.MarshalIndent(logEntry, "", "  ")
    if err != nil {
        fmt.Printf("Error marshalling log entry: %v\n", err)
        return 0, err
    }

    fmt.Println(string(jsonLog))
    return len(p), nil
}

