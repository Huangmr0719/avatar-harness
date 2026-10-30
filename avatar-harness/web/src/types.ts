export interface ProviderState { kind: string; model: string; configured: boolean; status: string; api_key_env: string; base_url: string; live_input?: boolean }
export interface Renderer { kind: 'none' | 'scene' | 'lam' | 'iframe' | 'video'; asset_url?: string; url?: string; includes_audio?: boolean }
export interface Character { id: string; name: string; asset_path: string; voice: string; persona: string; pad?: Record<string, number>; renderer: Renderer }
export interface Tool { name: string; kind: string; description: string; timeout_ms: number }
export interface State { version: string; runtime: string; providers: Record<string, ProviderState>; characters: Character[]; tools: Tool[]; limits: Record<string, number>; oac_url: string }
export interface Session { id: string; character_id: string; title: string; created_at: number }
export interface HarnessEvent { seq: number; session_id: string; turn_id: string | null; type: string; at: number; data: Record<string, any> }
export interface Entry { id: string; at: number; kind: 'user' | 'assistant' | 'tool' | 'notice'; text: string; turn_id?: string; name?: string; status?: string; args?: unknown; result?: unknown; source?: string; elapsed_ms?: number; draft?: boolean }
export interface MotionFrame { offset_ms?: number; pts_ms?: number; blendshapes?: Record<string, number>; expressions?: Record<string, number>; head_quaternion?: number[] }
export interface AudioSegment { pts_ms: number; start_ms: number; duration_ms: number }
