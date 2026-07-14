export interface ComposerSendSettlement {
  releaseImages: boolean;
  restoreDraft: boolean;
}

/** Decide how the composer should settle its optimistic clear after sending. */
export function composerSendSettlement(
  sent: boolean,
  targetSessionId: string | null,
  currentSessionId: string | null,
): ComposerSendSettlement {
  if (sent) {
    return { releaseImages: true, restoreDraft: false };
  }

  return {
    releaseImages: false,
    restoreDraft: targetSessionId !== null && currentSessionId === targetSessionId,
  };
}
