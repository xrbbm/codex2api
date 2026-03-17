package main

import (
	"codex2api/handler"
	"codex2api/store"
	"fmt"
	"log"
	"net/http"
	"os"
)

var version = "dev"

const helpText = `
codex2api - 将 OpenAI Codex 转换为 Anthropic / OpenAI 兼容接口

用法:
  codex2api [选项]

选项:
  -h, --help    显示此帮助信息
  -p, --port	使用指定端口号
  -v            显示版本号
 
管理面板:
  启动后访问 http://localhost:13698/admin
  首次使用需设置管理员密码，然后上传 auth.json 并生成 API Key。

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
API 使用示例（将 <KEY> 替换为你在管理面板生成的 API Key）
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

  ① 查询可用模型
      curl http://localhost:13698/v1/models \
        -H "Authorization: Bearer <KEY>"

  ② Anthropic 兼容接口（/v1/messages）- 非流式
      curl http://localhost:13698/v1/messages \
        -H "Content-Type: application/json" \
        -H "x-api-key: <KEY>" \
        -H "anthropic-version: 2023-06-01" \
        -d '{
          "model": "gpt-5.2",
          "max_tokens": 1024,
          "messages": [
            {"role": "user", "content": "你好，介绍一下你自己"}
          ]
        }'

  ③ Anthropic 兼容接口（/v1/messages）- 流式
      curl http://localhost:13698/v1/messages \
        -H "Content-Type: application/json" \
        -H "x-api-key: <KEY>" \
        -H "anthropic-version: 2023-06-01" \
        -d '{
          "model": "gpt-5.2",
          "max_tokens": 1024,
          "stream": true,
          "messages": [
            {"role": "user", "content": "用 Go 写一个冒泡排序"}
          ]
        }'

  ④ OpenAI 兼容接口（/v1/chat/completions）- 非流式
      curl http://localhost:13698/v1/chat/completions \
        -H "Content-Type: application/json" \
        -H "Authorization: Bearer <KEY>" \
        -d '{
          "model": "gpt-5.2",
          "messages": [
            {"role": "system", "content": "你是一个有帮助的助手"},
            {"role": "user", "content": "1+1等于几？"}
          ]
        }'

  ⑤ OpenAI 兼容接口（/v1/chat/completions）- 流式
      curl http://localhost:13698/v1/chat/completions \
        -H "Content-Type: application/json" \
        -H "Authorization: Bearer <KEY>" \
        -d '{
          "model": "gpt-5.2",
          "stream": true,
          "messages": [
            {"role": "user", "content": "写一首关于秋天的诗"}
          ]
        }'

  ⑥ 健康检查
      curl http://localhost:13698/health

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
接入第三方客户端（如 Claude Code）
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

  export ANTHROPIC_BASE_URL=http://localhost:13698
  export ANTHROPIC_API_KEY=<KEY>
  claude

  或 OpenAI 客户端:
  export OPENAI_BASE_URL=http://localhost:13698/v1
  export OPENAI_API_KEY=<KEY>
`

func main() {
	args := os.Args[1:]
	port := ""

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-h", "--help":
			fmt.Print(helpText)
			return
		case "-v":
			fmt.Println("codex2api v" + version)
			return
		case "-p", "--port":
			if i+1 < len(args) {
				i++
				port = args[i]
			} else {
				fmt.Fprintln(os.Stderr, "错误: -p/--port 需要指定端口号")
				os.Exit(1)
			}
		}
	}

	dataDir := os.Getenv("DATA_DIR")
	if dataDir == "" {
		home, _ := os.UserHomeDir()
		dataDir = home + "/.codex2api"
	}

	db, err := store.New(dataDir)
	if err != nil {
		log.Fatalf("failed to init store: %v", err)
	}

	if port == "" {
		port = os.Getenv("PORT")
	}
	if port == "" {
		port = "13698"
	}

	mux := http.NewServeMux()
	h := handler.New(db)
	h.Register(mux)
 
	fmt.Printf("CODEX2API\nv%s listening on :%s\n", version, port)
	fmt.Printf("Admin panel: http://localhost:%s/admin\n", port)
	log.Fatal(http.ListenAndServe(":"+port, mux))
}
