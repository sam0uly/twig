-- A bounded ring of recent keys.
local Cache = {}
Cache.__index = Cache

local DEFAULTS = { capacity = 16, verbose = false }

function Cache.new(opts)
  local self = setmetatable({}, Cache)
  self.capacity = (opts and opts.capacity) or DEFAULTS.capacity
  self.slots = {}
  return self
end

function Cache:push(key)
  if #self.slots >= self.capacity then
    table.remove(self.slots, 1)
  end
  self.slots[#self.slots + 1] = key
end

--[[ The long form embeds the docs.
     It can span several lines. ]]
local doc = [[
  cache:push("one")
  cache:push("two")
]]

for i = 1, 0x10 do
  Cache.new({ capacity = i }):push(string.format("%d", i))
end

return Cache
