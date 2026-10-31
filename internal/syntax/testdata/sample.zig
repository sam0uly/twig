const std = @import("std");

/// A bounded ring of recent keys.
pub fn Cache(comptime capacity: usize) type {
    return struct {
        const Self = @This();

        slots: [capacity][]const u8 = undefined,
        head: usize = 0,

        pub fn push(self: *Self, key: []const u8) void {
            self.slots[self.head % capacity] = key;
            self.head += 1;
        }

        pub fn full(self: *const Self) bool {
            return self.head >= capacity;
        }
    };
}

pub fn main() !void {
    var cache = Cache(16){};
    cache.push("one");
    const mask: u8 = 0xff & 0b1111;

    // A multiline string line starts with a double backslash.
    const doc =
        \\Cache holds the most recent keys.
        \\Numbers print in hex: 0x10.
    ;
    std.debug.print("{s}\n", .{doc});
    _ = mask;
}
