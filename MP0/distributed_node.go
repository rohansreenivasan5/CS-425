package main

import (
    "bufio"
    "fmt"
    "net"
    "os"
)

func main() {
    if len(os.Args) != 4 {
        fmt.Println("Usage: node <node name> <logger address> <logger port>")
        os.Exit(1)
    }

    nodeName := os.Args[1]
    loggerAddress := os.Args[2]
    loggerPort := os.Args[3]

    // Connect to the logger
    conn, err := net.Dial("tcp", loggerAddress+":"+loggerPort)
    if err != nil {
        fmt.Println("Error connecting to logger:", err.Error())
        os.Exit(1)
    }
    defer conn.Close()
	_, err = conn.Write([]byte(nodeName + "\n"))
	if err != nil {
		fmt.Println("Error sending node name to logger:", err.Error())
		os.Exit(1)
	}
   reader := bufio.NewReader(os.Stdin)

    for {
        input, err := reader.ReadString('\n')
        if err != nil {
            fmt.Println("Error reading from stdin:", err.Error())
            break
        }

        // Format the data with the node name and send it to the logger
        _, err = conn.Write([]byte(nodeName + " " + input))
        if err != nil {
            fmt.Println("Error sending data to logger:", err.Error())
            break
        }
    }
}
