package main

import (
	"bufio"
	"encoding/gob"
	"fmt"
	"log"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
    "time"
    "sort"
)

type Command struct {
	TxnID       int64
	Sender      string
	Coordinator string
	Transaction string
	isClient bool
}
type Account struct {
    Balance          int64
    Mutex            sync.Mutex
    CreatorTxnID     int64
    ReadTimestamps   []int64
    TentativeWrites  map[int64]*Transaction // Changed from int64 to *Transaction
    ID string
    LastWriter int64
    // Cond             *sync.Cond
}
type Server struct {
	ID              string
	Peers           map[string]*ServerConnection
	Listener        net.Listener
	AccountBalances map[string]int64
	Transactions    map[int64]*Transaction
	Mutex           sync.Mutex
    Accounts        map[string]*Account // Maps account ID to Account struct
    AbortedTransactions map[int64]bool // Tracks aborted transactions
}

type ServerConnection struct {
	Address  string
	Encoder  *gob.Encoder
	Decoder  *gob.Decoder
	Conn     net.Conn
}

type Transaction struct {
	TxnID         int64
	Commands      []Command
	CommittedFlag bool
    ClientConn    *ServerConnection // Connection to the client that initiated this transaction
    TentativeSum  int64             // Sum of tentative writes to be committed
    NetChanges    map[string]int64 // Map account IDs to net changes
    AffectedBranches map[string]bool                // affected branch true
    BranchCommitStatus map[string]bool  // Branch ID to commit status
    CommitOKStatus     map[string]bool // New map to track "COMMIT OK" status per branch
}

func main() {
    branchID := os.Args[1]
    configFile := os.Args[2]

    server := initializeServer(branchID, configFile)
    go func() {
        fmt.Printf("Server %s listening on %s\n", server.ID, server.Listener.Addr().String())
        for {
            conn, err := server.Listener.Accept()
            if err != nil {
                log.Printf("Failed to accept connection: %s", err)
                continue
            }
            log.Printf("Accepted connection from %s\n", conn.RemoteAddr().String())
			
            go handleConnection(server, conn)
        }
    }()

    // Wait for a signal or other condition to terminate to keep the server running
    select {}
}

func initializeServer(branchID, configFile string) *Server {
    server := &Server{
        ID:              branchID,
        Peers:           make(map[string]*ServerConnection),
        AccountBalances: make(map[string]int64),
        Accounts: make(map[string]*Account),
        Transactions:    make(map[int64]*Transaction),
        AbortedTransactions:  make(map[int64]bool), // Tracks aborted transactions

    }

    configs, err := readConfig(configFile)
    if err != nil {
        log.Fatalf("Error reading config file: %s", err)
        return nil
    }

    listener, err := net.Listen("tcp", configs[branchID].Address)
    if err != nil {
        log.Fatalf("Unable to listen on address %s: %s", configs[branchID].Address, err)
        return nil
    }
    server.Listener = listener

    var wg sync.WaitGroup
    for id, cfg := range configs {
        if id == branchID {
            continue
        }
        wg.Add(1)
        go func(id string, cfg *ServerConnection) {
            defer wg.Done()
            connectToPeer(server, id, cfg)
        }(id, cfg)
    }
    wg.Wait() // Wait for all connections to be established

    return server
}

func readConfig(filename string) (map[string]*ServerConnection, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	configs := make(map[string]*ServerConnection)

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
		configs[id] = &ServerConnection{Address: address}
	}
	return configs, nil
}

func connectToPeer(server *Server, id string, config *ServerConnection) {
    if _, exists := server.Peers[id]; exists {
        log.Printf("Already connected to peer %s at %s\n", id, config.Address)
        return
    }

    var conn net.Conn
    var err error
    retries := 5  // Number of retries
    for i := 0; i < retries; i++ {
        conn, err = net.Dial("tcp", config.Address)
        if err == nil {
            break
        }
        log.Printf("Retry %d: Failed to connect to peer %s at %s. Error: %s\n", i+1, id, config.Address, err)
        time.Sleep(time.Second * 2)  // Wait for 2 seconds before retrying
    }

    if err != nil {
        log.Printf("Failed to connect to peer %s at %s after %d attempts\n", id, config.Address, retries)
        return
    }

    log.Printf("Succeeded to connect to peer %s at %s\n", id, config.Address)
    config.Conn = conn
    config.Encoder = gob.NewEncoder(conn)
    config.Decoder = gob.NewDecoder(conn)
    server.Peers[id] = config
}

func handleConnection(server *Server, conn net.Conn) {
    decoder := gob.NewDecoder(conn)
    for {
        var cmd Command
        if err := decoder.Decode(&cmd); err != nil {
            log.Printf("Error decoding command: %s", err)
            break
        }
        log.Printf("Received command: %+v", cmd)

        // Check if the transaction exists or create a new one
        txn, exists := server.Transactions[cmd.TxnID]
        if !exists {
            txn = &Transaction{
                TxnID:     cmd.TxnID,
                ClientConn: &ServerConnection{Conn: conn, Encoder: gob.NewEncoder(conn), Decoder: gob.NewDecoder(conn)},
            }
            server.Transactions[cmd.TxnID] = txn
        }

        // Process the command
        processCommand(server, cmd)
    }
}


func processCommand(server *Server, cmd Command) {
    //log.Printf("Received command: %+v", cmd)
    if _, aborted := server.AbortedTransactions[cmd.TxnID]; aborted {
        log.Printf("Ignoring command for aborted transaction %d", cmd.TxnID)
        return
    }
    validSenders := map[string]bool{"A": true, "B": true, "C": true, "D": true, "E": true}

    // Assuming we have an additional field to indicate if the command is from a client
    if _, ok := validSenders[cmd.Sender]; !ok { // GETTING MESSAGE FROM CLIENT
        switch strings.Fields(cmd.Transaction)[0] { // Extract the command type
        case "BEGIN":
            // Initialize a new transaction struct for the command
            txn := server.Transactions[cmd.TxnID]
            txn.Commands = []Command{cmd}
            txn.CommittedFlag = false

            // Respond to client with OK to acknowledge the BEGIN
            respondToClient(cmd, "OK", server)

        case "DEPOSIT", "WITHDRAW", "BALANCE":
            // Add command to transaction's command list
            if txn, ok := server.Transactions[cmd.TxnID]; ok {
                txn.Commands = append(txn.Commands, cmd)
                // Forward the command to the appropriate branch server
                forwardCommandToBranch(server, cmd)
            } else {
                // Transaction not found, possibly aborted or never started
                respondToClient(cmd, "NOT FOUND, ABORTED", server)
				//TODO: DO NOT ACCEPT ANY MORE OF THIS TXN ID
            }

        case "COMMIT":
            if txn, ok := server.Transactions[cmd.TxnID]; ok {
                txn.Commands = append(txn.Commands, cmd)
                // Calculate the number of affected branches
                affectedBranches := make(map[string]bool)
                for _, command := range txn.Commands {
                    parts := strings.Fields(command.Transaction)
                    if len(parts) > 1 {
                        branchID := parts[1][:1] // Assuming branchID is the first character of accountID
                        affectedBranches[branchID] = true
                    }
                }
                txn.AffectedBranches = affectedBranches
                txn.BranchCommitStatus = make(map[string]bool, len(txn.AffectedBranches))
                if commitTransaction(server, txn) {
                    //respondToClient(cmd, "COMMIT OK", server)
                } else {
                    respondToClient(cmd, "ABORTED", server)
                }
            } else {
                respondToClient(cmd, "ABORTED", server)
            }
        

        case "ABORT":
            txn, exists := server.Transactions[cmd.TxnID]
            if exists {
                abortTransaction(server, txn)
                respondToClient(cmd, "Transaction aborted successfully", server)
            } else {
                respondToClient(cmd, "Transaction not found", server)
            }
           
        }
    } else {
        // Handle internal server-to-server commands (for simplicity, not implemented here)
        //log.Printf("Message came from a server not a client")
        //IF we get a command from a server NOT a client, then check if we are the coord for that cmd. If not process as we are the intended branch
        if server.ID == cmd.Coordinator {
            switch cmd.Transaction {
            case "COMMIT READY":
                if txn, exists := server.Transactions[cmd.TxnID]; exists {
                    txn.BranchCommitStatus[cmd.Sender] = true // Record that this branch is ready to commit
        
                    // Check if all required branches have sent "COMMIT READY"
                    allReady := true
                    for branchID, ready := range txn.AffectedBranches {
                        if ready { // Check only branches that should report
                            if status, found := txn.BranchCommitStatus[branchID]; !found || !status {
                                allReady = false
                                break
                            }
                        }
                    }
        
                    if allReady {
                        // Send "COMMIT NOW" to all relevant branches
                        for branchID := range txn.AffectedBranches {
                            if peer, exists := server.Peers[branchID]; exists {
                                sendCommitNow(peer, txn, cmd)
                            }
                        }
                        currBranchAffected := txn.AffectedBranches[server.ID]
                        if currBranchAffected{
                            handleCommitNow(server, cmd)
                        }
                        // TODO this must be contingent on prev txns passing
                        //respondToClient(cmd, "COMMIT OK", server)
                    }
                }
        
            case "ABORT READY":
                // One of the branches has indicated it cannot commit
                server.AbortedTransactions[cmd.TxnID] = true // Mark the transaction as aborted
                abortTransaction(server, server.Transactions[cmd.TxnID])
                // for branchID := range txn.AffectedBranches {
                //     if peer, exists := server.Peers[branchID]; exists {
                //         sendAbort(peer, server.Transactions[cmd.TxnID])
                //     }
                // }
                respondToClient(cmd, "ABORTED", server)

            case "COMMIT WAIT":
                // print that we got a commit wait from a server
                log.Printf("Received COMMIT WAIT from server %s for transaction %d", cmd.Sender, cmd.TxnID)

            case "COMMIT OK":
                log.Printf("Received COMMIT OK from server %s for transaction %d", cmd.Sender, cmd.TxnID)
                txn, exists := server.Transactions[cmd.TxnID]
                if exists {
                    // Initialize CommitOKStatus map if it doesn't exist
                    if txn.CommitOKStatus == nil {
                        txn.CommitOKStatus = make(map[string]bool)
                    }
                    
                    // Mark the sender's branch as having committed successfully
                    txn.CommitOKStatus[cmd.Sender] = true
            
                    // Check if all affected branches have sent "COMMIT OK"
                    allReady := true
                    for branchID, _ := range txn.AffectedBranches {
                        if !txn.CommitOKStatus[branchID] {
                            allReady = false
                            break
                        }
                    }
            
                    if allReady {
                        log.Printf("All branches confirmed COMMIT OK for transaction %d", cmd.TxnID)
                        respondToClient(cmd, "COMMIT OK", server)
                    }
                } else {
                    log.Printf("Transaction %d not found when processing COMMIT OK", cmd.TxnID)
                }
            case "NOT FOUND, ABORTED":
                log.Printf("Account not found for transaction %d", cmd.TxnID)

                // Mark the transaction as aborted
                server.AbortedTransactions[cmd.TxnID] = true

                // Abort the transaction
                abortTransaction(server, server.Transactions[cmd.TxnID])

                // Respond to the client
                respondToClient(cmd, "NOT FOUND, ABORTED", server)

            
            default:
                // Process any other commands normally
                //processInternalCommand(server, cmd)
                if strings.HasPrefix(cmd.Transaction, "BALANCE") {
                    // Handle the case where cmd.Transaction starts with "BALANCE" but is not exactly "BALANCE"
                    // You can add your specific logic here, for example:
                    log.Printf("Received BALANCE command: %s", cmd.Transaction)
                    parts := strings.Fields(cmd.Transaction)
                    if len(parts) > 1 {
                        accountID := parts[1]
                        balance := parts[3]  // Assuming the balance value follows the account ID in the transaction string
                        balanceInfo := fmt.Sprintf("BALANCE %s = %s", accountID, balance)
                        respondToClient(cmd, balanceInfo, server)
                        // Additional handling based on further details of the command
                    } 
                }else {
                        // Process any other commands normally
                        log.Printf("Hit default case")
                        respondToClient(cmd, "OK", server)
                }
            }
        } else {
            // Normal processing for non-coordinator servers
            processInternalCommand(server, cmd)
        }
    }
}

func forwardCommandToBranch(server *Server, cmd Command) {
    accountID := strings.Fields(cmd.Transaction)[1]  // e.g., "A.foo"
    branchID := accountID[:1]                        // e.g., "A"

    if branchID == server.ID {
        // Handle command internally if it's meant for this server
        log.Printf("Handling command internally for account %s", accountID)
        cmd.Sender = server.ID
        processInternalCommand(server, cmd)
        return
    }
    cmd.Sender = server.ID
    log.Printf("Sending to Branch server %s", branchID)
    if branchServer, ok := server.Peers[branchID]; ok {
        go func() {
            if err := branchServer.Encoder.Encode(cmd); err != nil {
                log.Printf("Failed to send command to branch %s: %s", branchID, err)
            }
        }()
    } else {
        log.Printf("Branch server %s not found", branchID)
    }
    return
}

func processInternalCommand(server *Server, cmd Command) {

    parts := strings.Fields(cmd.Transaction)
    if len(parts) == 1 && parts[0] == "COMMIT" {
        handleCommit(server, cmd)
        return
    }else if parts[0] == "COMMIT" && parts[1] == "NOW" {
        handleCommitNow(server, cmd)
        return
    }else if parts[0] == "ABORT" {
        handleAbort(server, cmd)
        return
    } else if parts[0] == "BALANCE" {
        log.Printf("Handling Balance as server not coord")
        handleBalance(server, cmd, parts[1])
        return
    }


    if len(parts) < 3 {
        respondToClient(cmd, "INVALID COMMAND FORMAT", server)
        return
    }

    commandType, accountID, amountStr := parts[0], parts[1], parts[2]
    amount, err := strconv.ParseInt(amountStr, 10, 64)
    if err != nil {
        respondToClient(cmd, "INVALID AMOUNT", server)
        return
    }
    // Depending on the type of command, handle it accordingly
    if cmd.Coordinator != server.ID{
        server.Transactions[cmd.TxnID].Commands = append(server.Transactions[cmd.TxnID].Commands, cmd)

    }
    switch commandType {
    case "DEPOSIT":
        log.Printf("Handling deposit as server not coord")
        // Assume function handleDeposit exists to process deposits
        handleDeposit(server, cmd, accountID, amount)
    case "WITHDRAW":
        // Assume function handleWithdraw exists to process withdrawals
        log.Printf("Handling Widthdraw as server not coord")

        handleWithdraw(server, cmd, accountID, amount)
    }
    // Simulate response to client as these are internal and synchronous
    //respondToClient(cmd, "OK", server)
}

func handleAbort(server *Server, cmd Command) {
    // Mark the transaction as aborted
    server.AbortedTransactions[cmd.TxnID] = true
    
    // Remove the transaction from all accounts' TentativeWrites
    for _, account := range server.Accounts {
        account.Mutex.Lock()
        if _, exists := account.TentativeWrites[cmd.TxnID]; exists {
            if account.CreatorTxnID == cmd.TxnID {
                account.Balance = -1 // Mark account as invalid if it was created by this transaction
            }
            delete(account.TentativeWrites, cmd.TxnID)
            // account.Cond.Broadcast()

        }
        account.Mutex.Unlock()
    }
    // for _, account := range server.Accounts {
    //     account.Cond.Broadcast()  // Similarly, wake up all waiting goroutines
    // }
    // Respond to the client indicating the transaction has been aborted
    respondToServer(server, cmd.Sender, cmd, "ABORT")
}

func handleCommit(server *Server, cmd Command) {
    log.Printf("starting handle commit")
    txn, exists := server.Transactions[cmd.TxnID]
    if !exists {
        respondToServer(server, cmd.Sender, cmd, "Transaction not found")
        return
    }

    if _, aborted := server.AbortedTransactions[cmd.TxnID]; aborted {
        respondToServer(server, cmd.Sender, cmd, "Transaction already aborted")
        return
    }

    // Initialize the NetChanges map
    if txn.NetChanges == nil {
        txn.NetChanges = make(map[string]int64)
    }

    // Calculate the net change for each account involved in the transaction
    log.Printf("commands: %v", txn.Commands)

    for _, command := range txn.Commands {
        parts := strings.Fields(command.Transaction)
        if len(parts) < 3 {
            continue // Skip improperly formatted commands
        }

        accountID := parts[1]
        amount, err := strconv.ParseInt(parts[2], 10, 64)
        if err != nil {
            respondToServer(server, cmd.Sender, cmd, "Invalid amount format")
            return
        }
        function := parts[0]
        if function == "DEPOSIT"{
            txn.NetChanges[accountID] += amount
        }else{
            txn.NetChanges[accountID] -= amount
        }
        
    }

    // Verify net changes against current balances
    commitReady := true
    for accountID, netChange := range txn.NetChanges {
        log.Printf("accountid %s and net change : %d", accountID, netChange)

        branchID := accountID[:1]
        if branchID != server.ID {
            continue // Skip accounts that do not belong to this branch
        }
        account, exists := server.Accounts[accountID]
        if !exists {
            respondToServer(server, cmd.Sender, cmd, "Account not found: "+accountID)
            commitReady = false
            break
        }

        //account.Mutex.Lock()
        if account.Balance + netChange < 0 { // Check for non-negative balance
            commitReady = false
            respondToServer(server, cmd.Sender, cmd, "ABORT READY")
            return
        }
        //account.Mutex.Unlock()

        if !commitReady {
            break
        }
    }

    // If all checks pass, proceed with commit readiness protocol
    if commitReady {
        if server.ID == cmd.Coordinator {
            if txn, exists := server.Transactions[cmd.TxnID]; exists {
                txn.BranchCommitStatus[cmd.Sender] = true // Record that this branch is ready to commit
    
                // Check if all required branches have sent "COMMIT READY"
                allReady := true
                for branchID, ready := range txn.AffectedBranches {
                    if ready { // Check only branches that should report
                        if status, found := txn.BranchCommitStatus[branchID]; !found || !status {
                            allReady = false
                            break
                        }
                    }
                }
    
                if allReady {
                    // Send "COMMIT NOW" to all relevant branches
                    for branchID := range txn.AffectedBranches {
                        if peer, exists := server.Peers[branchID]; exists {
                            sendCommitNow(peer, txn, cmd)
                        }
                    }
                    currBranchAffected := txn.AffectedBranches[server.ID]
                        if currBranchAffected{
                            handleCommitNow(server, cmd)
                        }
                    //respondToClient(cmd, "COMMIT OK", server)
                }
            }
        } else {
            // Non-coordinating servers send "COMMIT READY" to the coordinator
            sendCommitReady(server.Peers[cmd.Coordinator], txn, server, cmd)
        }
    } else {
        // Handle rollback if not ready
        //abortTransaction(server, txn)
        respondToClient(cmd, "ABORT READY", server)
    }
}

func handleCommitNow(server *Server, cmd Command) {
    log.Printf("Processing COMMIT NOW for transaction ID %d", cmd.TxnID)
    txn, exists := server.Transactions[cmd.TxnID]
    if !exists {
        log.Printf("Transaction not found for COMMIT NOW: %d", cmd.TxnID)
        return
    }

    // List all affected accounts for this transaction within this branch
    var affectedAccounts []*Account
    for accountID, _ := range txn.NetChanges {
        if accountID[:1] == server.ID { // Check if the account is in this branch
            account, exists := server.Accounts[accountID]
            if exists {
                affectedAccounts = append(affectedAccounts, account)
            }
        }
    }

    // Collect all transaction IDs from tentative writes of affected accounts
    txnIDs := make([]int64, 0)
    txnMap := make(map[int64][]*Account) // Change to a slice of accounts
    for _, account := range affectedAccounts {
        for txnID := range account.TentativeWrites {
            txnIDs = append(txnIDs, txnID)
            txnMap[txnID] = append(txnMap[txnID], account)
        }
    }
    log.Printf("Before sorting tentative transaction IDs: %v", txnIDs)

    // Sort transaction IDs
    sort.Slice(txnIDs, func(i, j int) bool { return txnIDs[i] < txnIDs[j] })
    log.Printf("After sorting tentative transaction IDs: %v", txnIDs)

    // Set the committed flag for the current transaction
    txn.CommittedFlag = true

    if len(txnIDs) == 0{
        // If there are no other tentative transactions, respond with COMMIT OK immediately
        log.Printf("No other transactions pending, committing transaction ID %d", cmd.TxnID)
        respondToServer(server, cmd.Sender, cmd, "COMMIT OK")
        return
    }
    //currentTxnCommitted := false
    // Commit transactions in order until an uncommitted transaction is found
    for _, txnID := range txnIDs {
        if otherTxn, ok := server.Transactions[txnID]; ok && otherTxn.CommittedFlag {
            for _, account := range txnMap[txnID] {
                account.Mutex.Lock()
                // Apply changes and remove from tentative writes
                change := txn.NetChanges[account.ID]
                if account.Balance + change < 0 {
                    log.Printf("COMMIT NOW FAILED AT COMMIT TIME for transaction ID %d", cmd.TxnID)
                    delete(account.TentativeWrites, txnID)
                    respondToServer(server, cmd.Sender, cmd, "ABORT READY")
                    account.Mutex.Unlock()
                    break
                }
                account.Balance += change
                log.Printf("ACCOUNT %s has balance %d ",account.ID, account.Balance)
                account.LastWriter = txnID
                coord := otherTxn.Commands[0].Coordinator
                log.Printf("COMMIT NOW processed for transaction ID %d", cmd.TxnID)
                // account.Cond.Broadcast()
                respondToServer(server, coord, otherTxn.Commands[0], "COMMIT OK")

                delete(account.TentativeWrites, txnID)
                account.Mutex.Unlock()
            }
            
            // if otherTxn.TxnID == cmd.TxnID {
            //     currentTxnCommitted = true
            // }
        } else {
            return // Stop if a transaction is not committed yet
        }
    }

    // for _, account := range affectedAccounts {
    //     account.Cond.Broadcast()  // Wake up all goroutines waiting on this account's condition variable
    // }

    // for _, txnID := range txnIDs {
    //     account := txnMap[txnID]
    //     log.Printf("ACCOUNT %s has balance %d ",account.ID, account.Balance)

    // }


    // if currrentTxnCommitted == true{
    //     log.Printf("COMMIT NOW processed for transaction ID %d", cmd.TxnID)
    //     respondToServer(server, cmd.Sender, cmd, "COMMIT OK")
    // } else{
    //     log.Printf("COMMIT NOW transaction ID %d IS WAITING in TW list", cmd.TxnID)
    //     respondToServer(server, cmd.Sender, cmd, "COMMIT WAIT")

    // }
}

func commitTransaction(server *Server, txn *Transaction) bool {
    affectedServers := make(map[string]bool) // Track unique servers involved

    // Determine all unique servers involved in this transaction
    for _, cmd := range txn.Commands {
        parts := strings.Fields(cmd.Transaction)
        if len(parts) < 2 {
            continue // Skip commands that don't conform to expected formatting
        }
        accountID := parts[1]
        branchID := accountID[:1]
        affectedServers[branchID] = true
    }

    // Prepare and send commit command to all involved servers
    commitCmd := Command{
        TxnID:       txn.TxnID,
        Sender:      server.ID,
        Coordinator: server.ID,
        Transaction: "COMMIT",
        isClient:    false, // This is a server-to-server communication
    }

    for branchID := range affectedServers {
        if branchID == server.ID {
            // Skip sending commit to itself; handle internally 
            // TODO: RUN COMMIT LOGIC HERE
            handleCommit(server, commitCmd)
            continue
        }
        if branchServer, ok := server.Peers[branchID]; ok {
            go func(bs *ServerConnection) {
                if err := bs.Encoder.Encode(commitCmd); err != nil {
                    log.Printf("Failed to send commit to branch %s: %s", branchID, err)
                } else {
                    log.Printf("Commit sent to branch %s", branchID)
                }
            }(branchServer)
        } else {
            log.Printf("Branch server %s not found, cannot send commit", branchID)
            return false // Return false if any commit fails to send
        }
    }

    // Optionally handle internal commit here if necessary
    // E.g., Apply the final changes to the data store

    return true // Assuming commit is successful if all messages are sent
}

func abortTransaction(server *Server, txn *Transaction) {
    log.Printf("REahed aborttransaction")
    // Mark the transaction as aborted
    server.AbortedTransactions[txn.TxnID] = true
    log.Printf("REahed aborttransaction2")
    // Find all unique branches that participated in this transaction
    affectedBranches := make(map[string]bool)
    for _, cmd := range txn.Commands {
        parts := strings.Fields(cmd.Transaction)
        if len(parts) > 1 {
            accountID := parts[1]
            branchID := accountID[:1]
            affectedBranches[branchID] = true
        }
    }
    log.Printf("REahed aborttransaction3")

    // Prepare the abort command
    abortCmd := Command{
        TxnID:       txn.TxnID,
        Sender:      server.ID,
        Coordinator: server.ID,
        Transaction: "ABORT",
        isClient:    false,
    }
    log.Printf("REahed aborttransaction4")
    // Send the abort command to all affected branches
    for branchID := range affectedBranches {
        
        if branchServer, ok := server.Peers[branchID]; ok && branchID != server.ID {
            go func(bs *ServerConnection) {
                if err := bs.Encoder.Encode(abortCmd); err != nil {
                    log.Printf("Failed to send abort to branch %s: %s", branchID, err)
                }
            }(branchServer)
        }
    }
    log.Printf("REahed aborttransaction5")
    // Revert changes or delete accounts created by this transaction
    for _, account := range server.Accounts {
        account.Mutex.Lock()
        if _, exists := account.TentativeWrites[txn.TxnID]; exists {
            if account.CreatorTxnID == txn.TxnID {
                // This account was created by this transaction; delete or mark as invalid
                
                delete(server.Accounts, account.ID)
                log.Printf("Account %s created by transaction %d has been deleted", account.ID, txn.TxnID)
                account.Mutex.Unlock()
            } else {
                // Revert changes from this transaction
                // This may require restoring previous values from a log or snapshot if available
                account.Mutex.Unlock()
                log.Printf("Reverting changes to account %s made by transaction %d", account.ID, txn.TxnID)
                // delete(account.TentativeWrites, txn.TxnID)
            }
            delete(account.TentativeWrites, txn.TxnID)
        }
    }

    log.Printf("REahed aborttransaction6")
    log.Printf("Transaction %d aborted successfully", txn.TxnID)
}


func sendCommitReady(peer *ServerConnection, txn *Transaction, server *Server, cmd Command) {
    commitReadyCmd := Command{
        TxnID:       txn.TxnID,
        Sender:      server.ID,
        Coordinator: cmd.Coordinator,
        Transaction: "COMMIT READY",
        isClient:    false,
    }
    if err := peer.Encoder.Encode(commitReadyCmd); err != nil {
        log.Printf("Failed to send COMMIT READY to %s: %s", peer.Address, err)
    }
}
func sendCommitNow(peer *ServerConnection, txn *Transaction, cmd Command) {
    commitNowCmd := Command{
        TxnID:       txn.TxnID,
        Sender:      cmd.Coordinator,
        Coordinator: cmd.Coordinator,
        Transaction: "COMMIT NOW",
        isClient:    false,
    }
    if err := peer.Encoder.Encode(commitNowCmd); err != nil {
        log.Printf("Failed to send COMMIT NOW to %s: %s", peer.Address, err)
    }
}


func respondToClient(cmd Command, response string, server *Server) {
    txn := server.Transactions[cmd.TxnID]
    if txn.ClientConn == nil || txn.ClientConn.Encoder == nil {
        log.Printf("No connection available to respond to client for transaction %d", txn.TxnID)
        return
    }
    // Construct a response message, you could define a proper struct for this
    
    // responseMsg := fmt.Sprintf("Response to client %s: %s", txn.ClientConn.Address, response)
    // log.Printf("reached respondtoclient")

    // Encode and send the response
    if err := txn.ClientConn.Encoder.Encode(response); err != nil {
        log.Printf("Failed to send response to client %s: %s", txn.ClientConn.Address, err)
    } else {
        log.Printf("Response sent to client %s: %s", txn.ClientConn.Address, response)
    }
}

func handleDeposit(server *Server, cmd Command, accountID string, amount int64) {
    if amount <= 0 {
        respondToServer(server, cmd.Sender, cmd, "INVALID AMOUNT: Deposit amount must be positive")
        return
    }

    server.Mutex.Lock()
    account, exists := server.Accounts[accountID]
    if !exists {
        newMutex := &sync.Mutex{}
        // Initialize the account if it does not exist
        account = &Account{
            Balance:         0,
            Mutex:           *newMutex,
            TentativeWrites: make(map[int64]*Transaction),
            CreatorTxnID:    cmd.TxnID, // Transaction that creates the account
            ID: accountID,
            // Cond:            sync.NewCond(newMutex),
        }
        server.Accounts[accountID] = account
    }else if account.Balance == -1 {
        account.Balance = 0 // Reset balance if previously marked as invalid
    }
    server.Mutex.Unlock()


    account.Mutex.Lock()
    if cmd.TxnID <= account.LastWriter || !isGreaterThanOrEqual(cmd.TxnID, account.ReadTimestamps) {
        account.Mutex.Unlock()
        respondToServer(server, cmd.Sender, cmd, "ABORT READY")
        // account.Mutex.Unlock()
        return
    }

    var waitingRequired bool
    for txnID, txn := range account.TentativeWrites {
        if txnID > account.LastWriter && txnID < cmd.TxnID && !txn.CommittedFlag {
            waitingRequired = true
        }
    }

    if waitingRequired {
        account.Mutex.Unlock()
        time.Sleep(10 * time.Millisecond) // Allow other transactions to commit
        handleDeposit(server, cmd, accountID, amount) // Retry deposit
        return
    }

    // account.Mutex.Lock()
    // defer account.Mutex.Unlock()


    // Log the deposit as a tentative write
    
    if txn, ok := account.TentativeWrites[cmd.TxnID]; ok {
        // If there is already a transaction record, update it
        txn.Commands = append(txn.Commands, cmd)
    } else {
        // Create a new transaction record for tentative writes
        account.TentativeWrites[cmd.TxnID] = &Transaction{
            TxnID:     cmd.TxnID,
            Commands:  []Command{cmd},
            CommittedFlag: false, // Initial state
        }
    }
    account.Mutex.Unlock()

    // Apply the deposit tentatively (could adjust based on commit/abort later)
   // account.Balance += amount

    // Respond with OK to the server that sent the command
    respondToServer(server, cmd.Sender, cmd, "OK")
}

func handleWithdraw(server *Server, cmd Command, accountID string, amount int64) {
    if amount <= 0 {
        respondToServer(server, cmd.Sender, cmd, "INVALID AMOUNT: Withdrawal amount must be positive")
        return
    }

    server.Mutex.Lock()
    account, exists := server.Accounts[accountID]
    if !exists || account.Balance == -1 {
        // Account does not exist, abort the transaction
        server.Mutex.Unlock() // Unlock server mutex before aborting
        respondToServer(server, cmd.Sender, cmd, "NOT FOUND, ABORTED")
        if txn, ok := server.Transactions[cmd.TxnID]; ok {
            abortTransaction(server, txn) // Abort transaction if account not found
        }
        return
    }
    server.Mutex.Unlock()
    
    account.Mutex.Lock()
    if cmd.TxnID <= account.LastWriter || !isGreaterThanOrEqual(cmd.TxnID, account.ReadTimestamps) {
        account.Mutex.Unlock()
        respondToServer(server, cmd.Sender, cmd, "ABORT READY")
        // account.Mutex.Unlock()
        return
    }


    var waitingRequired bool
    for txnID, txn := range account.TentativeWrites {
        if txnID > account.LastWriter && txnID < cmd.TxnID && !txn.CommittedFlag {
            waitingRequired = true
        }
    }

    if waitingRequired {
        account.Mutex.Unlock()
        time.Sleep(10 * time.Millisecond) // Allow other transactions to commit
        handleWithdraw(server, cmd, accountID, amount) // Retry withdrawal
        return
    }

    // If all checks pass, log the withdrawal as a tentative write
    // account.Mutex.Lock()
    if txn, ok := account.TentativeWrites[cmd.TxnID]; ok {
        // If there is already a transaction record, update it
        txn.Commands = append(txn.Commands, cmd)
        
    } else {
        // Create a new transaction record for tentative writes
        account.TentativeWrites[cmd.TxnID] = &Transaction{
            TxnID:     cmd.TxnID,
            Commands:  []Command{cmd},
            CommittedFlag: false, // Initial state
        }
    }
    account.Mutex.Unlock()

    // Respond with OK to the server that sent the command
    respondToServer(server, cmd.Sender, cmd, "WITHDRAW OK: Withdrawn successfully")
}

func isGreaterThanOrEqual(txnID int64, readTimestamps []int64) bool {
    for _, readTimestamp := range readTimestamps {
        if txnID < readTimestamp {
            return false
        }
    }
    return true
}

func handleBalance(server *Server, cmd Command, accountID string) {
    for {
        server.Mutex.Lock()
        account, exists := server.Accounts[accountID]
        server.Mutex.Unlock()

        if !exists || account.Balance == -1 {
            respondToServer(server, cmd.Sender, cmd, "NOT FOUND, ABORTED")
            return
        }
        account.Mutex.Lock()

        // Check transaction status again in case it's been aborted in the meantime

        if _, aborted := server.AbortedTransactions[cmd.TxnID]; aborted {
            account.Mutex.Unlock()
            respondToServer(server, cmd.Sender, cmd, "Transaction aborted")
            // account.Mutex.Unlock()
            return
        }

        // Get the transaction requesting the balance
        requestingTxn, txnExists := server.Transactions[cmd.TxnID]
        if !txnExists {
            account.Mutex.Unlock()
            respondToServer(server, cmd.Sender, cmd, "Transaction not found")
            // account.Mutex.Unlock()
            return
        }

        if requestingTxn.TxnID <= account.LastWriter {
            server.AbortedTransactions[cmd.TxnID] = true
            account.Mutex.Unlock()
            respondToServer(server, cmd.Sender, cmd, "ABORT READY")
            // account.Mutex.Unlock()
            return
        }

        if account.LastWriter == 0 {
            foundValidWrite := false
            for txnID := range account.TentativeWrites {
                if txnID <= requestingTxn.TxnID {
                    foundValidWrite = true
                    break
                }
            }
        
            // If no valid writes are found for the requesting transaction
            if !foundValidWrite {
                account.Mutex.Unlock()
                respondToServer(server, cmd.Sender, cmd, "NOT FOUND, ABORTED")
                // account.Mutex.Unlock()
                return
            }
        }

        var balance = account.Balance

        if _, ok := account.TentativeWrites[requestingTxn.TxnID]; ok {
            // Process its own tentative writes without waiting for other transactions
            for _, cmd := range account.TentativeWrites[requestingTxn.TxnID].Commands {
                if strings.HasPrefix(cmd.Transaction, "DEPOSIT") {
                    amount, _ := strconv.ParseInt(strings.Fields(cmd.Transaction)[2], 10, 64)
                    balance += amount
                } else if strings.HasPrefix(cmd.Transaction, "WITHDRAW") {
                    amount, _ := strconv.ParseInt(strings.Fields(cmd.Transaction)[2], 10, 64)
                    balance -= amount
                }
            }
            account.Mutex.Unlock()
            respondToServer(server, cmd.Sender, cmd, fmt.Sprintf("BALANCE %s = %d", accountID, balance))
            account.Mutex.Lock()
            account.ReadTimestamps = append(account.ReadTimestamps, requestingTxn.TxnID)
            account.Mutex.Unlock()
            return
        }

        // Check for any tentative write that's newer than the last committed but older than the current transaction
        var waitingRequired bool
        for tID, tTransaction := range account.TentativeWrites {
            if tID > account.LastWriter && tID < requestingTxn.TxnID && !tTransaction.CommittedFlag {
                waitingRequired = true
            }

            if (tID <= requestingTxn.TxnID || tTransaction.CommittedFlag) {
                for _, cmd := range tTransaction.Commands {
                    if strings.HasPrefix(cmd.Transaction, "DEPOSIT") {
                        amount, _ := strconv.ParseInt(strings.Fields(cmd.Transaction)[2], 10, 64)
                        balance += amount
                    } else if strings.HasPrefix(cmd.Transaction, "WITHDRAW") {
                        amount, _ := strconv.ParseInt(strings.Fields(cmd.Transaction)[2], 10, 64)
                        balance -= amount
                    }
                }
            }
        }

        if waitingRequired {
            account.Mutex.Unlock() // Unlock before sleeping
            time.Sleep(10 * time.Millisecond) // Sleep for 10 seconds to allow other transactions to commit
            continue // Continue will reacquire the lock at the start of the loop
        }

        // Respond with the calculated balance
        account.Mutex.Unlock()
        respondToServer(server, cmd.Sender, cmd, fmt.Sprintf("BALANCE %s = %d", accountID, balance))
        account.Mutex.Lock()
        account.ReadTimestamps = append(account.ReadTimestamps, requestingTxn.TxnID)
        account.Mutex.Unlock()
        return
    }
}






func respondToServer(server *Server, senderID string, cmd Command, response string) {
    if server.ID == cmd.Coordinator {
        log.Printf("Reached respond to server, WE are coordinator here")
        // If this server is the coordinator, respond directly to the client
        //if response != "COMMIT NOW"{
        if response == "NOT FOUND, ABORTED" {
            log.Printf("sending abort to client")
            server.AbortedTransactions[cmd.TxnID] = true

            // Abort the transaction
            abortTransaction(server, server.Transactions[cmd.TxnID])
            log.Printf("REahed respond to server post aborttransaction")

            // Respond to the client
            respondToClient(cmd, "NOT FOUND, ABORTED", server)
            return
        }
        log.Printf("Reached respond to server if2")
        respondToClient(cmd, response, server)
        //}
        
        
    } else {
        // Otherwise, send the response to the server that initiated the request
       
        peer, exists := server.Peers[senderID]
        if exists && peer.Conn != nil {
            log.Printf("Responding to server %s: %s", senderID, response)

            // Construct the response command to send back to the server
            responseCmd := Command{
                TxnID:       cmd.TxnID,
                Sender:      server.ID,
                Coordinator: cmd.Coordinator,
                Transaction: response,
                isClient:    false,
            }

            // Encode and send the response command
            if err := peer.Encoder.Encode(responseCmd); err != nil {
                log.Printf("Failed to send response to server %s: %s", senderID, err)
            }
        } else {
            log.Printf("No connection available to send response to server %s", senderID)
        }
    }
}