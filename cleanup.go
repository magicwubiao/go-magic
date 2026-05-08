package main

import (
	"fmt"
	"os"
)

func main() {
	err := os.Remove(`D:\project\go\go-magic\delete_tmp_file.go`)
	if err != nil {
		fmt.Printf("error: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("cleaned up")
}
