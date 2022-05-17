terraform {
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = ">= 4.5.0"
    }
  }
  required_version = ">= 1.1.7"
}

### Variables ###
variable "region" {
  type        = string
  description = "The region to run Oracle"
  default     = "us-east-1"
}

variable "security_group_name" {
  type        = string
  description = "The AMI ID for the Oracle node"
  default     = "ami-04505e74c0741db8d" # Ubuntu Server 20.04 LTS in us-east-1"
}

variable "ami_id" {
  type        = string
  description = "The AMI ID for the Oracle node"
  default     = "ami-04505e74c0741db8d" # Ubuntu Server 20.04 LTS in us-east-1"
}

variable "instance_type" {
  type        = string
  description = "The instance type for the Oracle node"
  default     = "t3.xlarge"
}

variable "keypair_name" {
  type        = string
  description = "Which AWS keypair to use to allow SSH access to your servers (see https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/ec2-key-pairs.html)"
  default     = "oracle-keypair"
}

provider "aws" {
  region = var.region
}

### Resources ###
resource "aws_security_group" "oracle-security-group"{
  name        = var.security_group_name
  description = "Allow inbound SSH traffic and all outbound traffic"
  ingress {
    from_port  = 22
    to_port    = 22
    protocol   = "tcp"
    cidr_blocks = ["0.0.0.0/0"]
  }
  egress {
    from_port  = 0
    to_port    = 0
    protocol   = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }
  tags = {
    Name = "Oracle Security Group"
  }
}

resource "aws_instance" "oracle" {
  provider               = aws
  count                  = 1
  ami                    = var.ami_id
  instance_type          = var.instance_type
  key_name               = var.keypair_name
  user_data              = ""
  vpc_security_group_ids = [aws_security_group.oracle-security-group.id]
  tags = {
    Name = "Oracle"
  }
}

### Outputs ###
output "inventory" {
  value = [for s in aws_instance.oracle[*] : {
    # the Ansible groups to which we will assign the server
    "name" : "${s.tags.Name}",
    "groups" : "oracle",
    "ip" : "${s.public_ip}",
    "ansible_ssh_user" : "ubuntu",
    "private_key_file" : "~/.ssh/${var.keypair_name}"
  }]
}
