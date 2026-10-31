# A bounded cache of recent keys.

"""
    Cache(capacity = 16)

Keep at most `capacity` keys, newest first.
"""
mutable struct Cache
    capacity::Int
    slots::Vector{String}
end

Cache(; capacity::Int = 16) = Cache(capacity, String[])

function push!(cache::Cache, key::String)
    if length(cache.slots) >= cache.capacity
        pop!(cache.slots)
    end
    pushfirst!(cache.slots, key)
end

Base.in(key::String, cache::Cache) = key in cache.slots

const MASK = 0xff & 0b1111

#=
Block comments nest, so #= an inner one =# is still a comment.
=#

report(c::Cache) = """{"kind": "cache", "size": $(length(c.slots))}"""
