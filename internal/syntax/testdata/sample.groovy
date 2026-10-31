// A bounded cache built with optional typing.
package fun.twig

import java.util.ArrayDeque

class Cache {
    private final ArrayDeque<String> slots = new ArrayDeque<>()
    final int capacity

    Cache(int capacity = 16) {
        this.capacity = capacity
    }

    void push(String key) {
        if (slots.size() >= capacity) slots.removeFirst()
        slots.addLast(key)
    }

    boolean contains(String key) {
        return slots.contains(key)
    }

    String report() {
        return """
        {"kind": "cache", "size": ${slots.size()}}
        """
    }

    /* Block comments do not nest,
       but they do span lines. */
    static final int MASK = 0xff & 0b1111
}
