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
local initial_index = next_index

repeat
    if is_proxy_healthy(next_index) then
        break
    end
    next_index = (next_index + 1) % proxy_count
until next_index == initial_index

-- If we've checked all proxies and none are healthy
if next_index == initial_index and not is_proxy_healthy(next_index) then
    return {-1, 0}
end

-- Generate endpoint key for the selected proxy
local endpoint_key = string.format("proxy_endpoints:%s:%d", proxy_hash, next_index)

-- If endpoint was used and still in cooldown, return index and ready timestamp
local last_used = redis.call('ZSCORE', endpoint_key, endpoint)
if last_used then
    last_used = tonumber(last_used)
    if (timestamp - last_used) < cooldown then
        local next_available = last_used + cooldown
        update_proxy_state(next_index, endpoint_key, next_available)
        return {next_index, next_available}
    end
end

-- Update state for immediate use
update_proxy_state(next_index, endpoint_key, timestamp)

-- Return the index of the available proxy with no wait time
return {next_index, timestamp}
