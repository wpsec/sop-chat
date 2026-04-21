/**
 * ShareChat Component
 * Displays a shared conversation by threadId (read-only)
 */
import React, { useState, useEffect } from 'react';
import { useParams, useSearchParams } from 'react-router-dom';
import Message from './Message';
import { getSharedThread, getSharedThreadMessages, getSharedEmployee } from '../services/api';
import { convertBackendMessages } from '../utils/chatMessages';

const ShareChat = () => {
  const { employeeName, threadId } = useParams();
  const [searchParams] = useSearchParams();
  const [employee, setEmployee] = useState(null);
  const [thread, setThread] = useState(null);
  const [messages, setMessages] = useState([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(null);
  const shareToken = searchParams.get('shareToken') || '';

  useEffect(() => {
    const loadData = async () => {
      try {
        setLoading(true);
        setError(null);

        const [threadData, employeeData, messagesData] = await Promise.all([
          getSharedThread(employeeName, threadId, shareToken),
          getSharedEmployee(employeeName, shareToken),
          getSharedThreadMessages(employeeName, threadId, shareToken),
        ]);
        
        // Convert backend message format to frontend format
        const convertedMessages = convertBackendMessages(messagesData);
        setThread(threadData);
        setEmployee(employeeData);
        setMessages(convertedMessages);
      } catch (err) {
        console.error('Failed to load shared conversation:', err);
        setError(err.message || '加载对话失败');
      } finally {
        setLoading(false);
      }
    };

    if (!shareToken) {
      setLoading(false);
      setError('分享链接无效或已失效');
      return;
    }

    if (employeeName && threadId) {
      loadData();
    }
  }, [employeeName, shareToken, threadId]);

  if (loading) {
    return (
      <div className="chat-window chat-window-shared">
        <div className="loading-center">
          <div className="loading-spinner"></div>
          <p>正在加载对话...</p>
        </div>
      </div>
    );
  }

  if (error) {
    return (
      <div className="chat-window chat-window-shared">
        <div className="error-center">
          <h3>加载失败</h3>
          <p>{error}</p>
        </div>
      </div>
    );
  }

  if (!employee || !thread) {
    return null;
  }

  const sharedCloudAccountId = thread.cloudAccountId || employee.cloudAccountId || '';

  return (
    <div className="chat-window chat-window-shared">
      <div className="chat-header chat-header-shared">
        <div className="header-left">
          <h2>分享的对话</h2>
          <span className="read-only-badge">只读</span>
        </div>
        <div className="header-center">
          <span className="thread-id">ID: {threadId}</span>
        </div>
        <div className="header-buttons">
          <button 
            className="new-session-button-shared"
            onClick={() => {
              // 在新窗口打开该SOP问答助手的新会话页面
              if (employeeName) {
                const params = new URLSearchParams();
                if (sharedCloudAccountId) {
                  params.set('cloudAccountId', sharedCloudAccountId);
                }
                window.open(`/#/chat/${employeeName}${params.toString() ? `?${params.toString()}` : ''}`, '_blank');
              } else {
                // 如果没有员工信息，打开首页
                window.open('/', '_blank');
              }
            }}
          >
            创建新会话
          </button>
        </div>
      </div>

      <div className="messages-container">
        {messages.length === 0 ? (
          <div className="welcome-message">
            <h3>此对话暂无消息</h3>
          </div>
        ) : (
          messages.map((message, index) => (
            <Message 
              key={index} 
              role={message.role} 
              content={message.content}
              events={message.events}
              isShared={true}
              assistantName={employee?.displayName || employee?.name}
            />
          ))
        )}
      </div>
    </div>
  );
};

export default ShareChat;
