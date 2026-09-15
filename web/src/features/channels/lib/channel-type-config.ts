/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { CHANNEL_TYPES } from '../constants'

// ============================================================================
// Channel Type Configuration
// ============================================================================

export interface ChannelTypeConfig {
  id: number
  name: string
  icon: string
  requiresOrganization?: boolean
  requiresRegion?: boolean
  supportedModels?: string[]
  hints?: {
    baseUrl?: string
    key?: string
    models?: string
    other?: string
  }
  validation?: {
    keyFormat?: RegExp
    keyMinLength?: number
  }
}

/**
 * Configuration for each channel type
 */
export const CHANNEL_TYPE_CONFIGS: Record<number, ChannelTypeConfig> = {
  1: {
    id: 1,
    name: CHANNEL_TYPES[1],
    icon: 'openai',
    requiresOrganization: true,
    hints: {
      key: 'Format: sk-...',
      models: 'gpt-4,gpt-4-turbo,gpt-3.5-turbo',
    },
    validation: {
      keyFormat: /^sk-/,
      keyMinLength: 20,
    },
  },
  3: {
    id: 3,
    name: CHANNEL_TYPES[3],
    icon: 'azure',
    requiresRegion: true,
    hints: {
      baseUrl: 'Azure OpenAI Endpoint',
      key: 'Azure API Key',
      models: 'Deployment names',
    },
  },
  14: {
    id: 14,
    name: CHANNEL_TYPES[14],
    icon: 'anthropic',
    hints: {
      key: 'Format: sk-ant-...',
      models: 'claude-3-opus,claude-3-sonnet,claude-3-haiku',
    },
  },
  24: {
    id: 24,
    name: CHANNEL_TYPES[24],
    icon: 'google',
    hints: {
      key: 'Google API Key',
      models: 'gemini-pro,gemini-pro-vision',
    },
  },
  41: {
    id: 41,
    name: CHANNEL_TYPES[41],
    icon: 'google',
    requiresRegion: true,
    hints: {
      key: 'Service account JSON or API key',
      models: 'gemini-pro,gemini-1.5-pro',
      other: 'Region config: {"default": "us-central1"}',
    },
  },
  43: {
    id: 43,
    name: CHANNEL_TYPES[43],
    icon: 'deepseek',
    hints: {
      key: 'DeepSeek API Key',
      models: 'deepseek-chat,deepseek-coder',
    },
  },
  20: {
    id: 20,
    name: CHANNEL_TYPES[20],
    icon: 'openrouter',
    hints: {
      key: 'OpenRouter API Key',
      models: 'Use model IDs from OpenRouter',
    },
  },
  56: {
    id: 56,
    name: CHANNEL_TYPES[56],
    icon: 'replicate',
    hints: {
      key: 'Replicate API Token',
      models: 'Replicate model IDs',
    },
  },
  58: {
    id: 58,
    name: CHANNEL_TYPES[58],
    icon: 'newapi',
    hints: {
      baseUrl: 'Fallback base URL',
      key: 'Used by route auth templates',
      models: 'Models exposed by this channel',
    },
  },
  59: {
    id: 59,
    name: CHANNEL_TYPES[59],
    icon: 'Sub2API',
    hints: {
      baseUrl: 'Sub2API gateway base URL',
      key: 'Sub2API API Key',
      models: 'Models fetched from upstream /v1/models',
    },
  },
  60: {
    id: 60,
    name: CHANNEL_TYPES[60],
    icon: 'NewAPI',
    hints: {
      baseUrl: 'Base URL is required for this channel type',
      key: 'Enter API key for this channel',
      models: 'Models',
    },
  },
  62: {
    id: 62,
    name: CHANNEL_TYPES[62],
    icon: 'HuggingFace',
    hints: {
      baseUrl: 'https://router.huggingface.co',
      key: 'HuggingFace access token (hf_...)',
      models: 'provider/model-name:policy (e.g., openai/gpt-oss-120b:fastest)',
    },
  },
  63: {
    id: 63,
    name: CHANNEL_TYPES[63],
    icon: 'XiaomiMiMo',
    hints: {
      baseUrl: 'https://api.xiaomimimo.com',
      key: 'API key (sk-...) or Token Plan key (tp-...)',
      models: 'mimo-v2.5-pro, mimo-v2.5-flash',
    },
  },
  64: {
    id: 64,
    name: CHANNEL_TYPES[64],
    icon: 'Meta',
    hints: {
      baseUrl: 'https://api.meta.ai',
      key: 'Meta API key from dev.meta.ai',
      models: 'Llama-4-Maverick, Llama-4-Scout, Llama-3.3-70B',
    },
  },
  65: {
    id: 65,
    name: CHANNEL_TYPES[65],
    icon: 'SenseNova',
    hints: {
      baseUrl: 'https://token.sensenova.cn',
      key: 'SenseNova API key (sk-...)',
      models: 'sensenova-6.8-flash-lite, DeepSeek V4 Pro',
    },
  },
  66: {
    id: 66,
    name: CHANNEL_TYPES[66],
    icon: 'Nvidia',
    hints: {
      baseUrl: 'https://integrate.api.nvidia.com',
      key: 'Nvidia API key (nvapi-...)',
      models: 'nvidia/llama-3.1-70b-instruct, nvidia/nemotron-3-ultra-550b',
    },
  },
  67: {
    id: 67,
    name: CHANNEL_TYPES[67],
    icon: 'StepFun',
    hints: {
      baseUrl: 'https://api.stepfun.com',
      key: 'StepFun API key (sk-...)',
      models: 'step-3.7-flash, step-3.5-flash',
    },
  },
  68: {
    id: 68,
    name: CHANNEL_TYPES[68],
    icon: 'Groq',
    hints: {
      baseUrl: 'https://api.groq.com/openai',
      key: 'Groq API key (gsk_...)',
      models: 'llama-3.3-70b-versatile, mixtral-8x7b-32768',
    },
  },
  69: {
    id: 69,
    name: CHANNEL_TYPES[69],
    icon: 'OpenCode',
    hints: {
      baseUrl: 'https://opencode.ai/zen/v1',
      key: 'OpenCode API key (sk-...)',
      models: 'gpt-5.6-luna, grok-4.6, deepseek-v4-pro, claude-opus-5',
    },
  },
}

/**
 * Get configuration for a channel type
 */
export function getChannelTypeConfig(type: number): ChannelTypeConfig {
  return (
    CHANNEL_TYPE_CONFIGS[type] || {
      id: type,
      name: CHANNEL_TYPES[type as keyof typeof CHANNEL_TYPES] || 'Unknown',
      icon: 'openai',
    }
  )
}

/**
 * Check if channel type requires organization field
 */
export function requiresOrganization(type: number): boolean {
  return CHANNEL_TYPE_CONFIGS[type]?.requiresOrganization || false
}

/**
 * Check if channel type requires region configuration
 */
export function requiresRegion(type: number): boolean {
  return CHANNEL_TYPE_CONFIGS[type]?.requiresRegion || false
}

/**
 * Get hints for channel type
 */
export function getChannelTypeHints(type: number) {
  return CHANNEL_TYPE_CONFIGS[type]?.hints || {}
}

/**
 * Validate API key format for channel type
 */
export function validateKeyFormat(type: number, key: string): boolean {
  const config = CHANNEL_TYPE_CONFIGS[type]
  if (!config?.validation) return true

  const { keyFormat, keyMinLength } = config.validation

  if (keyMinLength && key.length < keyMinLength) {
    return false
  }

  if (keyFormat && !keyFormat.test(key)) {
    return false
  }

  return true
}
