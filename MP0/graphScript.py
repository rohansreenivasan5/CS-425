import pandas as pd
import matplotlib.pyplot as plt
import numpy as np

# Define the file path
file_path = 'data2.txt'

# Read the data from the file
df = pd.read_csv(file_path, sep=" - ", header=None, names=["current_ts", "sent_ts", "size"], engine='python')

# Convert data types
df[["current_ts", "sent_ts", "size"]] = df[["current_ts", "sent_ts", "size"]].astype(float)

# Calculate delay ensuring non-negative values
df["delay"] = (df["sent_ts"] - df["current_ts"]).abs()

# Group by each second (rounded down current timestamp)
df["current_ts_rounded"] = df["current_ts"].apply(np.floor).astype(int)
delay_stats = df.groupby("current_ts_rounded")["delay"].agg(
    min="min", 
    max="max", 
    median="median", 
    percentile_90=lambda x: x.quantile(0.9)
)

# Calculate bandwidth in bits per second, then convert to Kilobits per second
bandwidth_stats = df.groupby("current_ts_rounded")["size"].sum() * 8 / 1000  # Kbps

# Determine the overall min and max time for the x-axis limits
min_time = df["current_ts_rounded"].min()
max_time = df["current_ts_rounded"].max()

# Create a new DataFrame to ensure a bar for every second
all_seconds = pd.DataFrame(index=np.arange(min_time, max_time + 1))
bandwidth_stats = all_seconds.join(bandwidth_stats, how='left').fillna(0)

# Number of seconds in the range
num_seconds = max_time - min_time + 1

# Plotting
fig, axes = plt.subplots(2, 1, figsize=(12, 10))

# Delay graph
delay_stats['seconds_since_start'] = np.arange(1, len(delay_stats) + 1)
delay_stats.plot(ax=axes[0], x='seconds_since_start', y=['min', 'max', 'median', 'percentile_90'])
axes[0].set_title("Delay Metrics Over Time")
axes[0].set_xlabel("Seconds since start")
axes[0].set_ylabel("Delay (seconds)")
axes[0].grid(True)
# axes[0].set_xlim(1, num_seconds)  # Set x-axis limits from 1 to n

# Bandwidth graph
bandwidth_stats.reset_index(inplace=True)
bandwidth_stats['seconds_since_start'] = np.arange(1, num_seconds + 1)
axes[1].bar(bandwidth_stats['seconds_since_start'], bandwidth_stats['size'], width=0.8, align='center')
axes[1].set_title("Bandwidth Usage Over Time")
axes[1].set_xlabel("Seconds since start")
axes[1].set_ylabel("Bandwidth (Kbps)")
axes[1].grid(True)
# axes[1].set_xlim(1, num_seconds)  # Set x-axis limits from 1 to n

# Adjust x-axis limits to 1 to 100
axes[0].set_xlim(1, 100)  # For the Delay graph

axes[1].set_xlim(1, 100)  # For the Bandwidth graph
axes[0].set_ylim(0, 0.012)


plt.tight_layout()
plt.show()
