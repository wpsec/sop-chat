const extractTextContent = (contents = []) => contents
  .filter((content) => content.type === 'text')
  .map((content) => content.value || '')
  .join('');

export const normalizeThreadPreview = (text = '', maxLength = 80) => {
  const normalized = text.replace(/\s+/g, ' ').trim();

  if (!normalized) {
    return '';
  }

  if (normalized.length <= maxLength) {
    return normalized;
  }

  return `${normalized.slice(0, maxLength).trimEnd()}...`;
};

export const extractFirstUserQuestionFromBackendMessages = (backendMessages = [], maxLength = 80) => {
  for (const message of backendMessages) {
    if ((message.role || '').toLowerCase() !== 'user') {
      continue;
    }

    const preview = normalizeThreadPreview(extractTextContent(message.contents || []), maxLength);
    if (preview) {
      return preview;
    }
  }

  return '';
};

const buildToolEvent = (tool = {}, timestampBase = Date.now()) => {
  const toolName = tool.name || 'unknown_tool';
  const toolStatus = tool.status || 'completed';
  const toolArgs = tool.arguments || {};
  const toolContents = tool.contents || [];

  let phase = 'success';
  let status = 'completed';
  let toolSuccess = true;

  if (toolStatus === 'start') {
    phase = 'start';
    status = 'calling';
  } else if (toolStatus === 'success' || toolStatus === 'completed') {
    phase = 'success';
    status = 'ready';
  } else if (toolStatus === 'fail' || toolStatus === 'error' || toolStatus === 'failed') {
    phase = 'fail';
    status = 'failed';
    toolSuccess = false;
  }

  let toolResult = null;
  if (toolContents.length > 0) {
    toolResult = {
      success: toolSuccess,
      contents: toolContents,
    };

    if (!toolSuccess) {
      const errorTexts = toolContents
        .filter((content) => content.type === 'text')
        .map((content) => content.value || '')
        .join('');

      toolResult.error = errorTexts || '工具调用失败';
    }
  } else if (!toolSuccess) {
    toolResult = {
      success: false,
      error: '工具调用失败',
    };
  }

  return {
    type: 'tool_call',
    data: {
      tool: toolName,
      args: toolArgs,
      status,
      phase,
      result: toolResult,
      success: toolSuccess,
    },
    timestamp: timestampBase,
  };
};

const buildContentEvents = (contents = [], timestampBase = Date.now()) => contents
  .filter((content) => content.type === 'text' && content.value)
  .map((content, index) => ({
    type: 'content',
    data: content.value,
    timestamp: timestampBase + index + 1,
  }));

export const convertBackendMessages = (backendMessages = []) => {
  const converted = [];
  let currentMessage = null;

  for (const msg of backendMessages) {
    const role = msg.role || 'user';

    if (role === 'user') {
      const textContent = extractTextContent(msg.contents || []);
      if (!textContent) {
        continue;
      }

      if (currentMessage && currentMessage.role === 'assistant') {
        converted.push(currentMessage);
        currentMessage = null;
      }

      converted.push({
        role: 'user',
        content: textContent,
      });
      continue;
    }

    const timestampBase = Date.now();
    const toolEvents = Array.isArray(msg.tools)
      ? msg.tools.map((tool, index) => buildToolEvent(tool, timestampBase + index * 100))
      : [];
    const contentEvents = buildContentEvents(msg.contents || [], timestampBase + toolEvents.length * 100);
    const nextEvents = [...toolEvents, ...contentEvents];

    if (nextEvents.length === 0) {
      const textContent = extractTextContent(msg.contents || []);
      if (textContent) {
        nextEvents.push({
          type: 'content',
          data: textContent,
          timestamp: timestampBase,
        });
      }
    }

    if (nextEvents.length === 0) {
      continue;
    }

    if (currentMessage && currentMessage.role === 'assistant') {
      currentMessage.events = [...(currentMessage.events || []), ...nextEvents];
      continue;
    }

    currentMessage = {
      role: 'assistant',
      events: nextEvents,
    };
  }

  if (currentMessage) {
    converted.push(currentMessage);
  }

  return converted;
};
