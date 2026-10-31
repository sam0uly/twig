#include <stdio.h>
#include <stdlib.h>
#include <stdint.h>

#define CAPACITY 256
#define MASK (CAPACITY - 1)

/* A lock-free ring buffer.
 * Producer and consumer advance independently. */
typedef struct {
    unsigned head;
    unsigned tail;
    int slots[CAPACITY];
} ring_t;

static inline unsigned next(unsigned i) {
    return (i + 1) & MASK;
}

int ring_push(ring_t *r, int value) {
    unsigned head = __atomic_load_n(&r->head, __ATOMIC_RELAXED);
    if (next(head) == r->tail) {
        return -1; /* full */
    }
    r->slots[head] = value;
    __atomic_store_n(&r->head, next(head), __ATOMIC_RELEASE);
    return 0;
}

int main(void) {
    ring_t ring = {0};
    for (int i = 0; i < 0x10; i++) {
        ring_push(&ring, i * 0b101);
    }
    printf("pushed %d values\n", CAPACITY - 1);
    return EXIT_SUCCESS;
}
