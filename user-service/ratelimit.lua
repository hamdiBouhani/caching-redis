-- Sliding-window rate limiter using a Redis sorted set.
-- KEYS[1] = key (e.g. "ratelimit:user:123")
-- ARGV[1] = now (ms since epoch)
-- ARGV[2] = window_ms
-- ARGV[3] = limit
-- Returns { allowed (0/1), remaining, reset_ms }

local key       = KEYS[1]
local now       = tonumber(ARGV[1])
local window_ms = tonumber(ARGV[2])
local limit     = tonumber(ARGV[3])

local window_start = now - window_ms

-- Remove entries older than the window
redis.call('ZREMRANGEBYSCORE', key, '-inf', window_start)

-- Count remaining entries
local count = redis.call('ZCARD', key)

local allowed = 0
local remaining = 0
local reset_ms = 0

if count < limit then
    -- Add current request. Use now as score; member must be unique.
    -- Append a random suffix so multiple requests at the same ms don't collide.
    local member = tostring(now) .. '-' .. tostring(math.random(1000000))
    redis.call('ZADD', key, now, member)
    allowed = 1
    remaining = limit - count - 1
else
    remaining = 0
end

-- Reset time = oldest entry + window_ms
local oldest = redis.call('ZRANGE', key, 0, 0, 'WITHSCORES')
if oldest[2] then
    reset_ms = math.floor(tonumber(oldest[2]) + window_ms)
else
    reset_ms = now + window_ms
end

-- Ensure the key expires even if no more requests come in
redis.call('PEXPIRE', key, window_ms + 1000)

return { allowed, remaining, reset_ms }