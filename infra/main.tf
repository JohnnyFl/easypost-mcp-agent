terraform {
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 6.0"
    }
  }
}

provider "aws" {
  region = var.region
}

variable "region" {
  type    = string
  default = "eu-central-1"
}

variable "image_tag" {
  type    = string
  default = "v1"
}

variable "easypost_api_key" {
  type      = string
  sensitive = true
}

variable "mcp_token" {
  type      = string
  sensitive = true
}

resource "aws_ecr_repository" "mcp" {
  name         = "easypost-mcp"
  force_delete = true
}

output "repository_url" {
  value = aws_ecr_repository.mcp.repository_url
}

data "aws_iam_policy_document" "assume" {
  statement {
    actions = ["sts:AssumeRole"]
    principals {
      identifiers = ["lambda.amazonaws.com"]
      type        = "Service"
    }
  }
}

resource "aws_iam_role" "lambda" {
  name               = "easypost-mcp-lambda"
  assume_role_policy = data.aws_iam_policy_document.assume.json
}

resource "aws_iam_role_policy_attachment" "logs" {
  policy_arn = "arn:aws:iam::aws:policy/service-role/AWSLambdaBasicExecutionRole"
  role       = aws_iam_role.lambda.name
}

resource "aws_lambda_function" "mcp" {
  function_name                  = "easypost-mcp"
  package_type                   = "Image"
  image_uri                      = "${aws_ecr_repository.mcp.repository_url}:${var.image_tag}"
  role                           = aws_iam_role.lambda.arn
  architectures                  = ["arm64"]
  timeout                        = 35
  memory_size                    = 256
  reserved_concurrent_executions = 5

  environment {
    variables = {
      EASYPOST_API_KEY = var.easypost_api_key
      MCP_TOKEN        = var.mcp_token
    }
  }
}

resource "aws_lambda_function_url" "mcp" {
  function_name      = "easypost-mcp"
  authorization_type = "NONE"
}

output "function_url" {
  value = aws_lambda_function_url.mcp.function_url
}