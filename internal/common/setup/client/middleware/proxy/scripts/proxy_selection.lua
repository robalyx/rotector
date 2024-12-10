local rotation_key = KEYS[1]
local last_success_key = KEYS[2]

local proxy_count = tonumber(ARGV[1])
local endpoint = ARGV[2]
local timestamp = tonumber(ARGV[3])
local cooldown = tonumber(ARGV[4])
local proxy_hash = ARGV[5]

-- Function to update Redis state for a proxy and endpoint
local function update_proxy_state(index, ep_key, ts)
    -- Add/update endpoint with timestamp
    redis.call('ZADD', ep_key, ts, endpoint)
    -- Store this proxy index for this endpoint
    redis.call('SET', last_success_key, index, 'EX', cooldown * 2)
    -- Store the next index for future requests
    redis.call('SET', rotation_key, (index + 1) % proxy_count)
end

-- Function to check if a proxy is healthy
local function is_proxy_healthy(index)
    local unhealthy_key = string.format("proxy_unhealthy:%s:%d", proxy_hash, index)
    return redis.call('EXISTS', unhealthy_key) == 0
end

-- Try to get the last successful index for this endpoint or fallback to current rotation
local start_index = tonumber(redis.call('GET', last_success_key) or redis.call('GET', rotation_key) or 0)

-- Get the next proxy index after the last successful one
local next_index = (start_index + 1) % proxy_count

-- Keep trying proxies until we find a healthy one or run out of options
local tries = proxy_count
while tries > 0 and not is_proxy_healthy(next_index) do
    next_index = (next_index + 1) % proxy_count
    tries = tries - 1
end

-- Special case: If all proxies are unhealthy, return error
if tries == 0 then
    return {-1, 0}
end

-- Generate endpoint key for the selected proxy
local endpoint_key = string.format("proxy_endpoints:%s:%d", proxy_hash, next_index)

-- Check if this endpoint was recently used by this proxy
local endpoints = redis.call('ZRANGE', endpoint_key, 0, -1, 'WITHSCORES')

-- Find the most recent usage of this endpoint
local last_used = nil
for i = 1, #endpoints, 2 do
    local ep = endpoints[i]
    local ts = tonumber(endpoints[i + 1])
    if ep == endpoint then
        last_used = ts
        break
    end
end

-- If endpoint was used and still in cooldown, return index and wait time
if last_used and (timestamp - last_used) < cooldown then
    local wait_time = cooldown - (timestamp - last_used)
    update_proxy_state(next_index, endpoint_key, timestamp + wait_time)
    return {next_index, wait_time}
end

-- Update state for immediate use
update_proxy_state(next_index, endpoint_key, timestamp)

-- Return the index of the available proxy with no wait time
return {next_index, 0}
