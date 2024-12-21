package main

import (
    "bufio"
    "fmt"
    "net"
    "os"
    "strings"
    "time"
)

func main() {
    port := ":1234"
    listener, err := net.Listen("tcp", port)
    if err != nil {
        fmt.Fprintln(os.Stderr, "Error listening:", err.Error())
        os.Exit(1)
    }
    defer listener.Close()
    fmt.Println("Listening on " + port)

    for {
        conn, err := listener.Accept()
        if err != nil {
            fmt.Fprintln(os.Stderr, "Error accepting:", err.Error())
            os.Exit(1)
        }
        go handleRequest(conn)
    }
}

func handleRequest(conn net.Conn) {
    defer conn.Close()
    //remoteAddr := conn.RemoteAddr().String()
    now := time.Now()

	unixTimestamp := float64(now.UnixNano()) / float64(time.Second)

    reader_name := bufio.NewReader(conn)

    nodeName, err := reader_name.ReadString('\n')
    if err != nil {
        fmt.Println("Error reading node name:", err.Error())
        return
    }
    nodeName = strings.TrimSpace(nodeName)

	fmt.Printf("%.6f - %s connected\n", unixTimestamp, nodeName)

	reader := bufio.NewReader(conn)
	for {
		str, err := reader.ReadString('\n')
		if err != nil {
			fmt.Printf("%.6f - %s disconnected\n", unixTimestamp, nodeName)
			break
		}

        parts := strings.SplitN(strings.TrimSpace(str), " ", 2)
        if len(parts) != 2 {
            fmt.Fprintln(os.Stderr, "Invalid data format received:", str)
            continue
        }

        
        nodeName := parts[0]
        eventData := parts[1]

        eventParts := strings.SplitN(eventData, " ", 2)
        if len(eventParts) != 2 {
            fmt.Fprintln(os.Stderr, "Invalid event data format:", eventData)
            continue
        }
        timestamp := eventParts[0]
        data := eventParts[1]

        // Printing in the desired format
        fmt.Printf("%s %s %s\n", timestamp, nodeName, data)
        //write to a file with the following:
        // current TS and {timestamp}
        // 

        file, err := os.OpenFile("data.txt", os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0644)
        if err != nil {
            fmt.Println("Error opening file:", err.Error())
            continue
        }
        //defer file.Close()
        now := time.Now()
	    currentUnixTimestamp := float64(now.UnixNano()) / float64(time.Second)
        _, err = file.WriteString(fmt.Sprintf("%.6f - %s - %d\n", currentUnixTimestamp, timestamp, len(str)))
        if err != nil {
            fmt.Println("Error writing to file:", err.Error())
        }
    }
}

