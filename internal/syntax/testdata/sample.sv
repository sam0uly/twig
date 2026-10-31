// counter
module counter(
  input logic clk,
  input logic rst,
  output logic [7:0] q
);
  always_ff @(posedge clk) begin
    if (rst) q <= 8'h00;
    else q <= q + 1;
  end
endmodule
