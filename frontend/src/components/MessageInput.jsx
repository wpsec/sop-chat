/**
 * MessageInput Component
 * Input field for user messages
 */
import React, { useState, useRef } from 'react';

const CANCEL_COMMANDS = new Set(['/取消', '/停止', '/abort']);

const MessageInput = ({ onSend, onStop, disabled, isGenerating, placeholder = '请输入您的问题...' }) => {
  const [input, setInput] = useState('');
  const textareaRef = useRef(null);
  const canSubmitWhileGenerating = CANCEL_COMMANDS.has(input.trim().toLowerCase());

  const handleSubmit = (e) => {
    e.preventDefault();
    if ((!disabled || canSubmitWhileGenerating) && input.trim()) {
      onSend(input.trim());
      setInput('');
    }
  };

  const handleStop = (e) => {
    e.preventDefault();
    onStop && onStop();
  };

  const handleKeyPress = (e) => {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault();
      if (!isGenerating || canSubmitWhileGenerating) {
        handleSubmit(e);
      }
    }
  };

  return (
    <form onSubmit={handleSubmit} className="message-input-form">
      <div className="input-row">
        <textarea
          ref={textareaRef}
          value={input}
          onChange={(e) => setInput(e.target.value)}
          onKeyPress={handleKeyPress}
          placeholder={placeholder}
          disabled={disabled && !isGenerating}
          rows="3"
          className="message-input"
        />
        {isGenerating ? (
          <button 
            type="button"
            onClick={handleStop}
            className="stop-button"
          >
            停止生成
          </button>
        ) : (
          <button 
            type="submit" 
            disabled={disabled || !input.trim()}
            className="send-button"
          >
            发送
          </button>
        )}
      </div>
    </form>
  );
};

export default MessageInput;
