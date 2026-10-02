import type { IssueStatus } from './api'

export interface ConversationIssue {
  message_id: string
  id: string
  public_id: string
  title: string
  status: IssueStatus
  merged_into_id?: string
}
