package main

import (
    "bufio"
    "encoding/gob"
    "fmt"
    "math/rand"
    "net"
    "os"
    "strconv"
    "strings"
    "sync"
    "time"
)

type Command struct {
    TxnID       int64
    Sender      string
    Coordinator string
    Transaction string
	isClient bool
}

type Transaction struct {
    TxnID         int64
    Sender        string
    Coordinator   string
    Commands      []string
    CommittedFlag bool
    ResponseChan  chan string
}

// Client manages all transactions and server communications
type Client struct {
    TxnMap   map[int64]*Transaction
    Servers  map[string]*ServerConnection
    ClientID string
    mutex    sync.Mutex
	currentTxn         int64
}

type ServerConnection struct {
    Address string
    Encoder *gob.Encoder
    Decoder *gob.Decoder
    Conn    net.Conn
}

func main() {
    if len(os.Args) < 3 {
        // fmt.Println("Usage: client <clientID> <configFile>")
        return
    }
    clientID := os.Args[1]
    configFile := os.Args[2]
    client := initializeClient(clientID, configFile)

    // fmt.Println("Client initialized and connections attempted to servers.")
    go client.handleInput()

    // Block main goroutine indefinitely
    select {}
}

func initializeClient(clientID, configFile string) *Client {
    servers, err := readConfig(configFile)
    if err != nil {
        // fmt.Println("Failed to read config:", err)
        os.Exit(1)
    }

    client := &Client{
        Servers:  servers,
        ClientID: clientID,
        TxnMap:   make(map[int64]*Transaction),
    }

    var wg sync.WaitGroup
    for id, server := range client.Servers {
        wg.Add(1)
        go func(id string, server *ServerConnection) {
            defer wg.Done()
            conn, err := net.Dial("tcp", server.Address)
            if err != nil {
                // fmt.Printf("Failed to connect to server %s at %s\n", id, server.Address)
                return
            }
            server.Conn = conn
            server.Encoder = gob.NewEncoder(conn)
            server.Decoder = gob.NewDecoder(conn)
        }(id, server)
    }
    wg.Wait()  // Wait for all goroutines to finish

    return client
}

func readConfig(filename string) (map[string]*ServerConnection, error) {
    file, err := os.Open(filename)
    if err != nil {
        return nil, err
    }
    defer file.Close()

    servers := make(map[string]*ServerConnection)
    scanner := bufio.NewScanner(file)
    for scanner.Scan() {
        parts := strings.Fields(scanner.Text())
        if len(parts) != 3 {
            continue
        }
        id := parts[0]
        host := parts[1]
        port, err := strconv.Atoi(parts[2])
        if err != nil {
            continue
        }
        address := fmt.Sprintf("%s:%d", host, port)
        servers[id] = &ServerConnection{Address: address}
    }
    return servers, nil
}
 func (c *Client) handleInput() {
    scanner := bufio.NewScanner(os.Stdin)
    for scanner.Scan() {
        line := scanner.Text()
        c.processCommand(line)
    }
}


func (c *Client) processCommand(command string) {
    time.Sleep(time.Second)  // Wait for 2 seconds before retrying
    parts := strings.Fields(command)
    if len(parts) == 0 {
        return
    }

    switch parts[0] {
    case "BEGIN":
        c.beginTransaction()
    case "DEPOSIT", "WITHDRAW":
        if len(parts) < 3 {
            // fmt.Println("Invalid command format.")
            return
        }
        c.processTransactionCommand(parts[0], parts[1], parts[2])
    case "BALANCE":
        if len(parts) < 2 {
            // fmt.Println("Invalid command format.")
            return
        }
        c.processBalanceCommand(parts[0], parts[1])
    case "COMMIT", "ABORT":
        c.endTransaction(parts[0])
    default:
        // fmt.Println("Unknown command")
    }
}

func (c *Client) beginTransaction() {
    txnID := time.Now().UnixNano() // txnID as int64
    coordinator := selectRandomCoordinator(c.Servers)
    
    c.mutex.Lock()
     
    
    transaction := &Transaction{
        TxnID:        txnID,
        Sender:       c.ClientID,
        Coordinator:  coordinator,
        Commands:     []string{}, // Initialize the Commands slice
        ResponseChan: make(chan string),
    }
	transaction.Commands = append(transaction.Commands, "BEGIN")
    c.TxnMap[txnID] = transaction
    c.currentTxn = txnID // Store the current transaction ID
	// txn := c.TxnMap[txnID]
	// txn = append(txn.Commands, "BEGIN") // Append command to transaction's Commands list


    // Send BEGIN to coordinator, async handling of responses
	c.mutex.Unlock()
    go c.sendCommandToCoordinator(Command{
        TxnID:       txnID, 
        Sender:      c.ClientID, 
        Coordinator: coordinator, 
        Transaction: "BEGIN",
		isClient: true,
    })
}


func (c *Client) processTransactionCommand(commandType, account, amount string) {
    c.mutex.Lock()
    

    txn, exists := c.TxnMap[c.currentTxn]
    if !exists {
        // fmt.Println("No active transaction.")
        return
    }

    commandStr := fmt.Sprintf("%s %s %s", commandType, account, amount)
    txn.Commands = append(txn.Commands, commandStr) // Append command to transaction's Commands list
	c.mutex.Unlock()
    cmd := Command{
        TxnID:       txn.TxnID,
        Sender:      c.ClientID,
        Coordinator: txn.Coordinator,
        Transaction: commandStr,
		isClient: true,
    }
    
    go c.sendCommandToCoordinator(cmd)
}

func (c *Client) processBalanceCommand(commandType, account string) {
    c.mutex.Lock()

    txn, exists := c.TxnMap[c.currentTxn]
    if !exists {
        // fmt.Println("No active transaction.")
        c.mutex.Unlock()
        return
    }

    commandStr := fmt.Sprintf("%s %s", commandType, account)
    txn.Commands = append(txn.Commands, commandStr) // Append command to transaction's Commands list
    c.mutex.Unlock()
    cmd := Command{
        TxnID:       txn.TxnID,
        Sender:      c.ClientID,
        Coordinator: txn.Coordinator,
        Transaction: commandStr,
        isClient: true,
    }

    go c.sendCommandToCoordinator(cmd)
}


func (c *Client) endTransaction(commandType string) {
    c.mutex.Lock()
    

    txn, exists := c.TxnMap[c.currentTxn]
    if !exists {
        // fmt.Println("No active transaction.")
        return
    }

	txn.Commands = append(txn.Commands, commandType) // Append command to transaction's Commands list

	c.mutex.Unlock()
    cmd := Command{
        TxnID:       txn.TxnID,
        Sender:      c.ClientID,
        Coordinator: txn.Coordinator,
        Transaction: commandType,
		isClient: true,
    }
    
    go c.sendCommandToCoordinator(cmd)
    txn.CommittedFlag = true // Set the transaction as committed
}


func (c *Client) sendCommandToCoordinator(command Command) {
    coordinator := c.Servers[command.Coordinator]
    if coordinator == nil || coordinator.Encoder == nil {
        // fmt.Printf("No connection or encoder to coordinator: %s\n", command.Coordinator)
        return
    }

    // Use the existing encoder to send the command
    if err := coordinator.Encoder.Encode(&command); err != nil {
        // fmt.Printf("Encoding error to %s: %s\n", command.Coordinator, err)
        return
    }

    // Start a goroutine to listen for the response using the existing connection
    go c.receiveServerResponse(coordinator, command.TxnID)
}

func (c *Client) receiveServerResponse(server *ServerConnection, txnID int64) {
	//TODO HANDLE RESPONSES
    var response string
    if err := server.Decoder.Decode(&response); err != nil {
        // fmt.Println("Decoding error:", err)
        return
    }
    // Process and modify the response
    if strings.HasPrefix(response, "BALANCE") {
        response = strings.TrimPrefix(response, "BALANCE ")
    } else if strings.HasPrefix(response, "WITHDRAW OK") || strings.HasPrefix(response, "DEPOSIT OK") {
        response = "OK"
    } else if strings.HasPrefix(response, "ABORT READY") {
        response = "ABORTED"
    } else if response == "Transaction aborted successfully" || response == "Transaction aborted" ||
               response == "Transaction not found" || response == "Transaction already aborted" {
        response = "ABORTED"
    }
    // Print the response or handle it accordingly
    fmt.Println(response)
    if txn, exists := c.TxnMap[txnID]; exists {
        txn.ResponseChan <- response
    } else {
        // fmt.Printf("Transaction ID %d not found in transaction map\n", txnID)
    }
}


func selectRandomCoordinator(servers map[string]*ServerConnection) string {
    var ids []string
    for id := range servers {
        ids = append(ids, id)
    }
    rand.Seed(time.Now().UnixNano())
    if len(ids) == 0 {
        return ""
    }
    return ids[rand.Intn(len(ids))]
}
