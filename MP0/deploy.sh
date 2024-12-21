#!/bin/bash

set -x

read -p "Enter SSH username: " ssh_username
read -sp "Enter SSH password: " ssh_password
echo

# Define the list of VMs
vms=(
  "sp24-cs425-1401.cs.illinois.edu"
  "sp24-cs425-1402.cs.illinois.edu"
  "sp24-cs425-1403.cs.illinois.edu"
  "sp24-cs425-1404.cs.illinois.edu"
  "sp24-cs425-1405.cs.illinois.edu"
  "sp24-cs425-1406.cs.illinois.edu"
  "sp24-cs425-1407.cs.illinois.edu"
  "sp24-cs425-1408.cs.illinois.edu"
  "sp24-cs425-1409.cs.illinois.edu"
)

# Function to execute tasks on VMs
execute_on_vm() {
  if sshpass -f <(echo "$ssh_password") ssh -o StrictHostKeyChecking=no "$ssh_username@$1" '
    cd ~/CS425/MP0 &&
    go build distributed_node.go &&
    go build centralized_logger.go

    if [ "$1" = "sp24-cs425-1401.cs.illinois.edu" ]; then
      timeout 120s ./centralized_logger
    else
      node_name="node$(echo $1 | grep -o -E '\''[0-9]+$'\'' | sed '\''s/^1//'\'')"
      timeout 120s bash -c "python -u generator.py 0.1 | ./distributed_node \$node_name 172.22.156.47 1234"
    fi
  '; then
    echo "Successfully executed commands on $1"
  else
    echo "Failed to execute commands on $1"
  fi
}

# Executing tasks
for vm in "${vms[@]}"; do
  execute_on_vm "$vm" &
done

wait
