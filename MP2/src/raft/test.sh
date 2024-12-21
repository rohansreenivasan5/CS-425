#!/bin/bash

command_to_run="go test -run 2B"
output_file="output.txt"
iteration=1

while true; do
    # Clear output file before running the command
    echo "" > "$output_file"

    # Run the command and append output to the file
    output=$(eval "$command_to_run" 2>&1)
    echo "Iteration $iteration Output:" >> "$output_file"
    echo "$output" >> "$output_file"

    # Output iteration number to console
    echo "Iteration $iteration completed. Output saved to $output_file"

    # Check if output contains "failed"
    if grep -q "failed" "$output_file"; then
        echo "Found 'failed' in output. Exiting..."
        break
    fi

    ((iteration++))
done