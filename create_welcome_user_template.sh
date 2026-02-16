#!/bin/bash

# Complete curl command to create Professional Project Member Welcome template
curl -X POST http://localhost:9000/api/email-templates \
  -H "Content-Type: application/json" \
  -H "Authorization: your-session-id" \
  -d '{
    "name": "Professional Project Member Welcome",
    "subject": "Welcome to {{project_name}} - Project Access Granted",
    "body": "Dear {{client_name}},\n\nWelcome to {{project_name}}! Your project member account has been successfully created.\n\n**Account Details:**\n- **Email:** {{email}}\n- **Password:** {{password}}\n- **Role:** {{role}}\n- **Employee ID:** {{employee_id}}\n\n**Project Access:**\nYou now have access to the project dashboard and can collaborate with your team members.\n\n**Security Reminder:**\nPlease change your password immediately after your first login for security purposes.\n\n**Getting Started:**\n1. Visit {{login_url}}\n2. Login with your credentials\n3. Explore the project dashboard\n4. Update your profile information\n\n**Support:**\nIf you need any assistance, please contact {{support_email}}.\n\nBest regards,\n{{company_name}} Team",
    "template_type": "welcome_admin",
    "is_default": false,
    "is_active": true,
    "variables": [
      {"key": "client_name", "description": "Project member full name"},
      {"key": "project_name", "description": "Project name"},
      {"key": "email", "description": "User email"},
      {"key": "password", "description": "User password"},
      {"key": "role", "description": "User role in project"},
      {"key": "employee_id", "description": "Employee ID"},
      {"key": "login_url", "description": "Login URL"},
      {"key": "support_email", "description": "Support email"},
      {"key": "company_name", "description": "Company name"}
    ]
  }'
