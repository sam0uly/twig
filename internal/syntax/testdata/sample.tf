resource "aws_instance" "web" {
  count         = 2
  ami           = "ami-1234"
  instance_type = "t3.micro"

  tags = {
    Name = "web-${count.index}"
  }

  /* a block comment
     over two lines */
  lifecycle {
    prevent_destroy = false
  }
}
