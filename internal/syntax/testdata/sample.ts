// Rate limiter used by the HTTP edge.
import { EventEmitter } from "node:events";

export interface Bucket {
  readonly tokens: number;
  refillAt: number;
}

const NANOS = 1_000_000;
const MAX = 0xff_ff;

/* A bucket refills on a timer,
   independently of its callers. */
export class Limiter extends EventEmitter implements Bucket {
  tokens = 0;
  refillAt = Date.now() + NANOS;

  constructor(private readonly rate: number = 10) {
    super();
  }

  take(n: number): boolean {
    if (this.tokens < n) return false;
    this.tokens -= n;
    return true;
  }
}

const pattern = /^[a-z]+\d*$/gi;
const template = `bucket ${MAX} at ${NANOS}`;
const mask = 0b1010 | 0o17;
