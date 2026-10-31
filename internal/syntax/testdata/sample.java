package fun.twig;

import java.util.List;
import java.util.Optional;

/** A cache that keeps at most sixteen recent keys. */
public final class LruCache {
    private final List<String> slots;

    public LruCache(List<String> slots) {
        this.slots = slots;
    }

    public Optional<String> get(String key) {
        return slots.stream().filter(key::equals).findFirst();
    }

    // Character and numeric literals in several bases.
    static boolean printable(char c) {
        return c >= 0x20 && c <= '~';
    }

    static String describe() {
        return """
               {
                 "kind": "lru",
                 "capacity": %d
               }
               """.formatted(0b10000);
    }

    /* A block comment
       spanning two lines. */
    static final long MASK = 0xff_ffL << 1;
}
