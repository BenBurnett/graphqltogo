const { ApolloServer } = require('@apollo/server');
const { expressMiddleware } = require('@apollo/server/express4');
const { ApolloServerPluginDrainHttpServer } = require('@apollo/server/plugin/drainHttpServer');
const { makeExecutableSchema } = require('@graphql-tools/schema');
const express = require('express');
const { useServer } = require('graphql-ws/use/ws'); // Updated import for graphql-transport-ws
const { WebSocketServer } = require('ws');
const { PubSub } = require('graphql-subscriptions');
const http = require('http');
const bodyParser = require('body-parser');
const cors = require('cors');
const gql = require('graphql-tag');

// Define the schema
const typeDefs = gql`
  type Query {
    hello: String
    errorQuery: String
    analyticsDeviceCount: Int
    analyticsTaskCount: Int
    analyticsPluginCount: Int
    analyticsFileSummary: FileSummary
  }

  type Mutation {
    echo(message: String!): String
    tokenAuth(username: String!, password: String!): AuthPayload
  }

  type Subscription {
    messageSent: String
    newActivity: ActivityPayload
  }

  type FileSummary {
    count: Int
  }

  type AuthPayload {
    token: String
  }

  type ActivityPayload {
    activity: Activity
  }

  type Activity {
    title: String
  }
`;

// Initialize PubSub for subscriptions
const pubSub = new PubSub();
const MESSAGE_SENT = 'MESSAGE_SENT';

// Define the resolvers
const resolvers = {
  Query: {
    hello: () => 'Hello, world!',
    errorQuery: () => {
      throw new Error('This is an error');
    },
    analyticsDeviceCount: (parent, args, context) => {
      if (!context.user) throw new Error('Unauthorized');
      return 42; // Replace with actual logic
    },
    analyticsTaskCount: (parent, args, context) => {
      if (!context.user) throw new Error('Unauthorized');
      return 10; // Replace with actual logic
    },
    analyticsPluginCount: (parent, args, context) => {
      if (!context.user) throw new Error('Unauthorized');
      return 5; // Replace with actual logic
    },
    analyticsFileSummary: (parent, args, context) => {
      if (!context.user) throw new Error('Unauthorized');
      return { count: 100 }; // Replace with actual logic
    },
  },
  Mutation: {
    echo: (_, { message }) => {
      pubSub.publish(MESSAGE_SENT, { messageSent: message });
      return message;
    },
    tokenAuth: (_, { username, password }) => {
      // Replace with actual authentication logic
      if (username === 'admin' && password === 'admin') {
        return { token: 'valid-token' };
      }
      throw new Error('Invalid credentials');
    },
  },
  Subscription: {
    messageSent: {
      subscribe: () => pubSub.asyncIterableIterator([MESSAGE_SENT]),
    },
    newActivity: {
      subscribe: (parent, args, context) => {
        if (!context.user) throw new Error('Unauthorized');
        return pubSub.asyncIterableIterator(['NEW_ACTIVITY']);
      },
    },
  },
};

// Create the schema
const schema = makeExecutableSchema({ typeDefs, resolvers });

// Create an Express app
const app = express();
const httpServer = http.createServer(app);

// Create the Apollo Server
const server = new ApolloServer({
  schema,
  context: ({ req }) => {
    const token = req.headers.authorization || '';
    const user = token === 'Bearer valid-token' ? { username: 'admin' } : null;
    return { user };
  },
  plugins: [ApolloServerPluginDrainHttpServer({ httpServer })],
});

async function startServer() {
  await server.start();
  app.use('/graphql', cors(), bodyParser.json(), expressMiddleware(server));

  // Create WebSocket server
  const wsServer = new WebSocketServer({
    server: httpServer,
    path: '/graphql',
  });

  // Add subscription support
  useServer({
    schema,
    context: (ctx) => {
      const token = ctx.connectionParams.Authorization;
      const user = token === 'Bearer valid-token' ? { username: 'admin' } : null;
      return { user };
    },
    onConnect: async (ctx) => {
      const token = ctx.connectionParams.Authorization;
      const user = token === 'Bearer valid-token' ? { username: 'admin' } : null;
      ctx.user = user;
      console.log(ctx.user ? 'Client connected': 'Client unauthorized');
      return !!ctx.user;
    },
    onDisconnect: (ctx) => {
      console.log('Client disconnected');
    },
    onSubscribe: (ctx, msg) => {
      console.log('Subscription started:', msg);
    },
    onNext: (ctx, msg, args, result) => {
      console.log('Subscription data');
    },
    onError: (ctx, msg, errors) => {
      console.log('Subscription error:', errors);
    },
    onComplete: (ctx, msg) => {
      console.log('Subscription completed');
    },
  }, wsServer);

  // Start the server
  httpServer.listen(4000, () => {
    console.log(`🚀 Server ready at http://localhost:4000/graphql`);
    console.log(`🚀 Subscriptions ready at ws://localhost:4000/graphql`);
  });
}

// Example of publishing new activity
setInterval(() => {
  pubSub.publish('NEW_ACTIVITY', {
    newActivity: {
      activity: {
        title: 'New Activity Title',
      },
    },
  });
}, 5000);

startServer();
