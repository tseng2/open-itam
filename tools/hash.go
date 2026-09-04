// 命令行小工具：生成 Argon2id hash
// 用法：go run tools/hash.go <密码>
package main

import (
	"fmt"
	"os"

	"itagent/internal/agent/password"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("usage: hash <password>")
		os.Exit(1)
	}
	h, err := password.Hash(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, "err:", err)
		os.Exit(1)
	}
	fmt.Println(h)
}
