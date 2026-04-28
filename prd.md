# Simple Agent Orchastration. 

A committed config based CLI that knows to read GH issues and PRs and manage coding agents by priority and availability. 

## What it does

1. Reads a config and knows to pull and manage GH issues by label, unassigned, etc... 
2. Trigger a coding agent based on the priority and config (max tasks) and current state (authenticated properly and has tokens...) 

Think - I have a GH issue, I select Claude, Claude is out of tokens, I fall back to codex, or gemini.

The orchastration is deterministic, the execution is agentic. 

## Inspirations 

Read https://github.com/ComposioHQ/agent-orchestrator for ideas on the config, I do not want an orchastrating agent, zero tokens goes on orchastration. 


Important! implementation wise I want to rely on https://github.com/coder/agentapi so we don't invent the wheel in terms of abstractions over coding agents, the main task is a schematic definition and very lean CLI that orchastrates GH CLI / commands with agentapi. 