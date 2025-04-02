--!df flags=allow-undeclared-keys
local rotation_key = KEYS[1]

local proxy_count = tonumber(ARGV[1])
local proxy_hash = ARGV[2]

-- Function to check if a proxy is healthy
local function is_proxy_healthy(index)
    local unhealthy_key = string.format("roverse_proxy_unhealthy:%s:%d", proxy_hash, index)
    return redis.call('EXISTS', unhealthy_key) == 0
end

-- Get the current rotation index
local current_index = tonumber(redis.call('GET', rotation_key) or 0)

-- Increment the index for the next request (round-robin)
local next_index = (current_index + 1) % proxy_count
redis.call('SET', rotation_key, next_index)

-- If the next proxy is unhealthy, try to find a healthy one
if not is_proxy_healthy(next_index) then
    local initial_index = next_index
    repeat
        next_index = (next_index + 1) % proxy_count
        if is_proxy_healthy(next_index) then
            redis.call('SET', rotation_key, next_index)
            break
        end
    until next_index == initial_index
    
    -- If we've checked all proxies and none are healthy
    if next_index == initial_index and not is_proxy_healthy(next_index) then
        return {-1}
    end
end

-- Return the selected proxy index
return {next_index} 