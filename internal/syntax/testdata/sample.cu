#include <cuda_runtime.h>

__global__ void add(int n, float *a, float *b, float *c) {
    int i = blockIdx.x * blockDim.x + threadIdx.x;
    if (i < n) c[i] = a[i] + b[i];
}

int main() {
    // launch
    dim3 grid(1), block(256);
    add<<<grid, block>>>(n, a, b, c);
    return 0;
}
