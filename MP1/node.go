package main

import (
	"bufio"
	"container/heap"
	"encoding/gob"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

type NodeConfig struct {
	ID       string
	Hostname string
	Port     int
}

type Message struct {
	Content     string
	SeqNo       float64
	FinalSeqNo  float64
	Deliverable bool
	ID          string
}

type Heartbeat struct {
	FromID string
	Time   time.Time
}

type PriorityQueue []*Message

func (pq PriorityQueue) Len() int { return len(pq) }

func (pq PriorityQueue) Less(i, j int) bool {
	if pq[i].Deliverable != pq[j].Deliverable {
		return pq[i].Deliverable
	}
	return pq[i].FinalSeqNo < pq[j].FinalSeqNo || (pq[i].FinalSeqNo == pq[j].FinalSeqNo && pq[i].ID < pq[j].ID)
}

func (pq PriorityQueue) Swap(i, j int) {
	pq[i], pq[j] = pq[j], pq[i]
}

func (pq *PriorityQueue) Push(x interface{}) {
	item := x.(*Message)
	*pq = append(*pq, item)
}

func (pq *PriorityQueue) Pop() interface{} {
	old := *pq
	n := len(old)
	item := old[n-1]
	*pq = old[0 : n-1]
	return item
}

type Node struct {
	ID            string // Ensure ID is of type string to match the NodeConfig and usage
	MsgChannel    chan *Message
	ProposedSeqNo float64
	Lock          sync.Mutex
	MsgQueue      PriorityQueue
	MsgHeapLock   sync.Mutex
	Address       string
	Peers         map[string]net.Conn
	Listener      net.Listener
}

func (n *Node) StartListening() {
	ln, err := net.Listen("tcp", n.Address)
	if err != nil {
		log.Fatal(err)
	}
	n.Listener = ln
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				log.Println(err)
				continue
			}
			go n.handleConnection(conn)
		}
	}()
}
func (n *Node) monitorHeartbeats() {
	ticker := time.NewTicker(time.Second)
	for range ticker.C {
		currentTime := time.Now()
		for id, lastHeartbeat := range n.LastHeartbeatReceived {
			if currentTime.Sub(lastHeartbeat) > 5*time.Second {
				// Detected a failure
				log.Printf("Node %s is considered failed\n", id)
				// Handle the failure (e.g., remove the node from peers, attempt reconnection)
			}
		}
	}
}

func (n *Node) handleConnection(conn net.Conn) {
	defer conn.Close()
	dec := gob.NewDecoder(conn)
	for {
		var hb Heartbeat
		if err := dec.Decode(&hb); err != nil {
			if err == io.EOF {
				// Connection closed gracefully
				log.Println("Connection closed by peer")
			} else {
				// Handle error (e.g., log it, mark the node as failed)
				log.Println("Failed to decode message:", err)
			}
			return
		}

		// Update last heartbeat received timestamp for the peer
		log.Printf("Received heartbeat from %s\n", hb.FromID)
		// Actual handling of the heartbeat goes here
	}
}

func (n *Node) multicastMessage(msg *Message) {
	for _, conn := range n.Peers {
		enc := gob.NewEncoder(conn)
		if err := enc.Encode(msg); err != nil {
			log.Println(err)
		}
	}
}

func (n *Node) continuouslyConnect(config NodeConfig) {
	for {
		conn, err := net.Dial("tcp", config.Hostname+":"+strconv.Itoa(config.Port))
		if err != nil {
			log.Printf("Failed to connect to node %s, retrying...\n", config.ID)
			time.Sleep(1 * time.Second)
			continue
		}
		log.Printf("Successfully connected to node %s\n", config.ID)
		n.Peers[config.ID] = conn
		go n.handleConnection(conn)
		return
	}
}

func newNode(id string, address string) *Node {
	pq := make(PriorityQueue, 0)
	heap.Init(&pq)
	return &Node{
		ID:         id,
		MsgChannel: make(chan *Message, 10),
		MsgQueue:   pq,
		Address:    address,
		Peers:      make(map[string]net.Conn),
	}
}

func readConfig(filePath string) ([]NodeConfig, error) {
	file, err := os.ReadFile(filePath) // Use os.ReadFile instead of ioutil.ReadFile
	if err != nil {
		return nil, err
	}

	var configs []NodeConfig
	lines := strings.Split(string(file), "\n")
	for _, line := range lines[1:] { // Skip the first line with the number of nodes
		parts := strings.Fields(line)
		if len(parts) != 3 {
			continue // Skip incorrect lines
		}
		port, _ := strconv.Atoi(parts[2])
		configs = append(configs, NodeConfig{
			ID:       parts[0],
			Hostname: parts[1],
			Port:     port,
		})
	}
	return configs, nil
}

func main() {
	// Parse command-line arguments
	identifierPtr := flag.String("identifier", "", "Unique identifier for this node")
	configFilePtr := flag.String("config", "", "Path to the configuration file")
	flag.Parse()

	if *identifierPtr == "" || *configFilePtr == "" {
		log.Fatal("Usage: ./mp1_node -identifier <nodeID> -config <path to config file>")
	}

	// Read the configuration file
	configs, err := readConfig(*configFilePtr)
	if err != nil {
		log.Fatalf("Error reading config file: %s", err)
	}

	// Find the configuration for this node
	var currentNodeConfig NodeConfig
	found := false
	for _, config := range configs {
		if config.ID == *identifierPtr {
			currentNodeConfig = config
			found = true
			break
		}
	}
	if !found {
		log.Fatalf("Node ID %s not found in configuration", *identifierPtr)
	}

	// Initialize this node
	currentNode := newNode(currentNodeConfig.ID, fmt.Sprintf("%s:%d", currentNodeConfig.Hostname, currentNodeConfig.Port))
	currentNode.StartListening()

	// Connect to other nodes
	for _, config := range configs {
		if config.ID != currentNodeConfig.ID {
			go currentNode.continuouslyConnect(config)
		}
	}

	// Process incoming transactions
	go currentNode.sendHeartbeats()
	go currentNode.monitorHeartbeats()

	go func() {
		scanner := bufio.NewScanner(os.Stdin)
		for scanner.Scan() {
			line := scanner.Text()
			// Process the line here
			processTransaction(line, currentNode)
		}
		if err := scanner.Err(); err != nil {
			log.Printf("Error reading from stdin: %v\n", err)
		}
	}()

	// Keep the main goroutine alive
	select {}
}

func processTransaction(line string, n *Node) {
	tokens := strings.Fields(line)
	if len(tokens) < 3 {
		fmt.Println("Invalid transaction format")
		return
	}

	// Create a new message and multicast it
	msg := &Message{
		Content:     line,
		ID:          n.ID,
		SeqNo:       0,     // This would be set to a proper value in a full implementation
		FinalSeqNo:  0,     // This would be set to a proper value in a full implementation
		Deliverable: false, // Initially set to false; will be updated during the ordering process
	}
	n.multicastMessage(msg)
	// Additional logic for processing the message would be added here
}

func (n *Node) sendHeartbeats() {
	ticker := time.NewTicker(time.Second)
	for range ticker.C {
		hb := Heartbeat{FromID: n.ID, Time: time.Now()}
		for id, conn := range n.Peers {
			enc := gob.NewEncoder(conn)
			if err := enc.Encode(hb); err != nil {
				log.Printf("Failed to send heartbeat to %s: %s\n", id, err)
				// Handle failure (e.g., remove from peers, attempt to reconnect)
			}
		}
	}
}

// current

//The code sets up the nodes and allows them to connect and communicate. Each node listens for incoming connections and attempts to connect to its peers.

// what needs to be done

// Implement the logic for assigning initial sequence numbers to messages based on some ordering criteria.
// Complete the consensus algorithm to agree upon final sequence numbers for messages (i.e., the ISIS algorithm steps).
// Handle the delivery of messages once their order is determined, ensuring that all nodes process messages in the same total order.
// Implement the transaction processing logic to update the node's state based on the received messages (e.g., updating account balances for deposit and transfer operations).
// Add error handling and recovery mechanisms, especially for network issues and node failures.
// Implement logging and monitoring to help with debugging and performance evaluation.
// Write tests to cover various scenarios, including message ordering, node failures, and network partitions.
