// 调试工具：生成密码 hash 或验证密码
// 用法: go run . hash <plain>     -> 输出 PHC 格式
//       go run . verify <plain> <hash>
package main

import (
	"fmt"
	"os"

	"itagent/internal/agent/password"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("usage:")
		fmt.Println("  hash <plain>")
		fmt.Println("  verify <plain> <hash>")
		os.Exit(1)
	}
	switch os.Args[1] {
	case "hash":
		h, err := password.Hash(os.Args[2])
		if err != nil {
			fmt.Fprintln(os.Stderr, "err:", err)
			os.Exit(1)
		}
		fmt.Println(h)
	case "verify":
		if password.Verify(os.Args[2], os.Args[3]) {
			fmt.Println("OK")
			os.Exit(0)
		}
		fmt.Fprintln(os.Stderr, "密码错误")
		os.Exit(1)
	default:
		fmt.Fprintln(os.Stderr, "unknown op")
		os.Exit(1)
	}
}
