/** The conversation header's primary label, intentionally kept to one line. */
export function ConversationTitle({ title }: { title: string | undefined }) {
  return (
    <div className="min-w-0 flex-1 truncate text-[14px] font-semibold tracking-[-0.015em]">
      {title || '会话'}
    </div>
  );
}
