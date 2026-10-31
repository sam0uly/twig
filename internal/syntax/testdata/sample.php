<?php

declare(strict_types=1);

namespace Twig;

/**
 * Caches recent entries.
 */
#[Attribute]
final class Cache
{
    /** @var list<string> */
    private array $slots = [];

    public function __construct(private readonly int $capacity = 16) {}

    public function push(string $key): void
    {
        if (count($this->slots) >= $this->capacity) {
            array_shift($this->slots);
        }
        $this->slots[] = $key;
    }

    public function report(): string
    {
        return <<<JSON
            {"kind": "cache", "size": {count($this->slots)}}
            JSON;
    }

    // Numbers in several bases.
    public const MASK = 0xff & 0b1111;
    private float $ratio = 1.5e3;
}
